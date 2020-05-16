package p2p

import (
	"BitTorrent/client"
	"BitTorrent/peers"
	"bytes"
	"crypto/sha1"
	"fmt"
	"log"
	"runtime"
)

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

	c.SendUnchoke()
	c.SendInterested()

	for pieceWork := range workQueue {
		if !c.Bitfield.HasPiece(pieceWork.index) {
			workQueue <- pieceWork // put piece back on the queue
			continue
		}

		// Download the piece
		buf, err := attempDownloadPiece(c, pieceWork)
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

		c.SendHave(pieceWork.index)
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

func attempDownloadPiece(c *client.Client, work *pieceWork) ([]byte, error) {
	return nil, nil
}

func (t *Torrent) calculateBoundsForPiece(index int) (begin, end int) {
	begin = index * t.PieceLength
	end = begin + t.PieceLength
	if end > t.Length {
		end = t.Length
	}
	return begin, end
}
