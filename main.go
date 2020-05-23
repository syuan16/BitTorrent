package Main

import (
	"github.com/syuan16/BitTorrent/torrentfile"
	"log"
	"os"
)

func main() {
	inPath := os.Args[1]
	outPath := os.Args[2]

	file, err := torrentfile.Open(inPath)
	if err != nil {
		log.Fatal(err)
	}

	err = file.DownloadToFile(outPath)
	if err != nil {
		log.Fatal(err)
	}
}
