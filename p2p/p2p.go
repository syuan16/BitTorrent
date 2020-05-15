package p2p

import (
	"BitTorrent/peers"
	"log"
	"runtime"
)

type Torrent struct {
	Peers       []peers.Peer
	PeerID      [20]byte
	InfoHash    [20]byte
	PieceHashes [][20]byte
	PieceLength int
	Length      int
	Name        string
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
	return 0
}

func (t *Torrent) startDownloadWorker(peer peers.Peer, queue chan *pieceWork, results chan *pieceResult) {

}

func (t *Torrent) calculateBoundsForPiece(index int) (int, int) {
	return 0, 0
}
