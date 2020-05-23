// the BitTorrent Tracker Protocol comes from https://wiki.theory.org/index.php/BitTorrent_Tracker_Protocol

package torrentfile

import (
	"BitTorrent/peers"
	"github.com/jackpal/bencode-go"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type bencodeTrackerResp struct {
	Interval int    `bencode:"interval"`
	Peers    string `bencode:"peers"`
}

func (t *TorrentFile) buildTrackerURL(peerID [20]byte, port uint16) (string, error) {
	base, err := url.Parse(t.Announce)
	if err != nil {
		return "", err
	}

	params := url.Values{
		"info_hash":  []string{string(t.InfoHash[:])},
		"peer_id":    []string{string(peerID[:])},
		"port":       []string{strconv.Itoa(int(port))},
		"uploaded":   []string{"0"},
		"downloaded": []string{"0"},
		"compact":    []string{"1"},
		"left":       []string{strconv.Itoa(t.Length)},
	}
	base.RawQuery = params.Encode()
	return base.String(), nil
}

// This function requests from tracker the list of peers while providing the tracker with its own peerID and port
func (t *TorrentFile) requestPeers(peerID [20]byte, port uint16) ([]peers.Peer, error) {
	url, err := t.buildTrackerURL(peerID, port)
	if err != nil {
		return nil, err
	}

	c := &http.Client{Timeout: 15 * time.Second}
	response, err := c.Get(url)
	if err != nil {
		return nil, err
	}

	defer response.Body.Close()

	trackerResponse := bencodeTrackerResp{}
	err = bencode.Unmarshal(response.Body, &trackerResponse)
	if err != nil {
		return nil, err
	}

	return peers.Unmarshal([]byte(trackerResponse.Peers))

	return nil, nil
}
