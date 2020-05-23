package p2p

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"github.com/syuan16/BitTorrent/client"
	"github.com/syuan16/BitTorrent/message"
	"github.com/syuan16/BitTorrent/peers"
	"log"
	"runtime"
	"time"
)

// the largest number of bytes in a block a request can ask for
// a piece is broken into blocks
const MaxBlockSize = 16384

// MaxBacklog is the number of unfulfilled requests a client can have in its pipeline
// pipeline can increase the throughput of the connection
// requesting block one by one will tank the performance of the download
// the value of 5 is a classical value, and increasing it can up to double the speed
// **adaptive** queue size can also be used, however for simplicity it will not
const MaxBacklog = 5

type Torrent struct {
	Peers       []peers.Peer
	PeerID      [20]byte   // the ID of the peer itself
	InfoHash    [20]byte   // uniquely identifies files when talking to trackers and peers
	PieceHashes [][20]byte // the hashes of the pieces of the file to be downloaded
	PieceLength int        // the length of each piece hash
	Length      int        // total length of the file
	Name        string     // file name
}

type pieceWork struct {
	index  int
	hash   [20]byte
	length int
}

type pieceResult struct {
	index int
	buf   []byte
}

// the struct that keeps track of each peer
type pieceProgress struct {
	index      int
	client     *client.Client
	buf        []byte
	downloaded int
	requested  int // the number of bytes that have been downloaded
	backlog    int
}

func (state *pieceProgress) readMessage() error {
	msg, err := state.client.Read() // this call blocks
	if err != nil {
		return err
	}

	if msg == nil { // keep-alive
		return nil
	}

	switch msg.ID {
	case message.MsgUnchoke:
		state.client.Choked = false
	case message.MsgChoke:
		state.client.Choked = true
	case message.MsgHave:
		index, err := message.ParseHave(msg)
		if err != nil {
			return err
		}
		state.client.Bitfield.SetPiece(index)
	case message.MsgPiece:
		n, err := message.ParsePieece(state.index, state.buf, msg)
		if err != nil {
			return err
		}
		state.downloaded += n
		state.backlog--
	}
	return nil
}

func (t *Torrent) Download() ([]byte, error) {
	log.Println("Starting to download for ", t.Name)

	// init queues for workers to retrieve work and send results
	workQueue := make(chan *pieceWork, len(t.PieceHashes))

	// init queue for buffering downloaded results from the workQueue
	results := make(chan *pieceResult)

	// put pieces to be worked into the workQueue
	for index, hash := range t.PieceHashes {
		length := t.calculatePieceSize(index)
		workQueue <- &pieceWork{index, hash, length}
	}

	// start workers
	for _, peer := range t.Peers {
		go t.startDownloadWorker(peer, workQueue, results)
	}

	// collect results into a buffer until full
	buf := make([]byte, t.Length)
	donePieces := 0
	for donePieces < len(t.PieceHashes) {
		res := <-results
		begin, end := t.calculateBoundsForPiece(res.index)
		copy(buf[begin:end], res.buf)
		donePieces++

		percent := float64(donePieces) / float64(len(t.PieceHashes)) * 100
		numWorkers := runtime.NumGoroutine() - 1 // minus one for main thread
		log.Printf("(%0.2f%%) Downloaded piece #%d from %d peers\n", percent, res.index, numWorkers)
	}

	close(workQueue)
	return buf, nil
}

func (t *Torrent) calculatePieceSize(index int) int {
	begin, end := t.calculateBoundsForPiece(index)
	return end - begin
}

func (t *Torrent) startDownloadWorker(peer peers.Peer, workQueue chan *pieceWork, results chan *pieceResult) {
	c, err := client.New(peer, t.PeerID, t.InfoHash)
	if err != nil {
		log.Printf("Could not handshake with %s. Disconnecting\n", peer.IP)
		return
	}

	defer c.Conn.Close()
	log.Printf("Completed handshake with %s\n", peer.IP)

	err = c.SendUnchoke()
	if err != nil {
		return
	}
	err = c.SendInterested()
	if err != nil {
		return
	}

	for pieceWork := range workQueue {

		// if the peer does not have this piece
		if !c.Bitfield.HasPiece(pieceWork.index) {
			workQueue <- pieceWork // put piece back on the queue
			continue
		}

		// Download the piece
		buf, err := attemptDownloadPiece(c, pieceWork)
		if err != nil {
			log.Println("Exiting", err)
			workQueue <- pieceWork
			return
		}

		err = checkIntegrity(pieceWork, buf)
		if err != nil {
			log.Printf("index %d does not match hash of piece", pieceWork.index)
			workQueue <- pieceWork
			continue
		}

		err = c.SendHave(pieceWork.index)
		if err != nil {
			return
		}

		results <- &pieceResult{index: pieceWork.index, buf: buf}
	}
}

func checkIntegrity(work *pieceWork, buf []byte) error {
	hash := sha1.Sum(buf)
	if !bytes.Equal(hash[:], work.hash[:]) {
		return fmt.Errorf("index %d does not match hash of piece", work.index)
	}
	return nil
}

func attemptDownloadPiece(c *client.Client, work *pieceWork) ([]byte, error) {
	state := pieceProgress{
		index:      work.index,
		client:     c,
		buf:        make([]byte, work.length),
		downloaded: 0,
		requested:  0,
		backlog:    0,
	}

	// Setting a deadline helps get unresponsive peers unstuck
	// 30 seconds is more than enough time to download a 262 KB piece
	err := c.Conn.SetDeadline(time.Now().Add(30 * time.Second))
	if err != nil {
		return nil, err
	}
	// disable the deadline
	defer c.Conn.SetDeadline(time.Time{})

	for state.downloaded < work.length {
		// if unchoked, send requests until we have enough unfulfilled requests
		if !state.client.Choked {
			for state.backlog < MaxBacklog && state.requested < work.length {
				blockSize := MaxBlockSize

				// Last block might be shorter than the typical block
				if work.length-state.requested < blockSize {
					blockSize = work.length - state.requested
				}

				err := c.SendRequest(work.index, state.requested, blockSize)
				if err != nil {
					return nil, err
				}

				state.backlog++
				state.requested += blockSize
			}
		}

		err := state.readMessage()
		if err != nil {
			return nil, err
		}
	}

	return state.buf, nil
}

func (t *Torrent) calculateBoundsForPiece(index int) (begin, end int) {
	begin = index * t.PieceLength
	end = begin + t.PieceLength
	if end > t.Length {
		end = t.Length
	}
	return begin, end
}
