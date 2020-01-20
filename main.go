package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"

	"gopkg.in/alecthomas/kingpin.v2"
)

var Version string = "v0.0.0"

type trackFlow int

const (
	_TRACK_FLOW_RTP trackFlow = iota
	_TRACK_FLOW_RTCP
)

type track struct {
	rtpPort  int
	rtcpPort int
}

type streamProtocol int

const (
	_STREAM_PROTOCOL_UDP = iota
	_STREAM_PROTOCOL_TCP
)

func (s streamProtocol) String() string {
	if s == _STREAM_PROTOCOL_UDP {
		return "udp"
	}
	return "tcp"
}

type program struct {
	protocols  map[streamProtocol]struct{}
	rtspPort   int
	rtpPort    int
	rtcpPort   int
	publishKey string
	mutex      sync.RWMutex
	rtspl      *rtspTcpListener
	rtpl       *rtspUdpListener
	rtcpl      *rtspUdpListener
	clients    map[*client]struct{}
	publishers map[string]*client
}

func newProgram(protocolsStr string, rtspPort int, rtpPort int, rtcpPort int, publishKey string) (*program, error) {
	protocols := make(map[streamProtocol]struct{})
	for _, proto := range strings.Split(protocolsStr, ",") {
		switch proto {
		case "udp":
			protocols[_STREAM_PROTOCOL_UDP] = struct{}{}

		case "tcp":
			protocols[_STREAM_PROTOCOL_TCP] = struct{}{}

		default:
			return nil, fmt.Errorf("unsupported protocol: %s", proto)
		}
	}
	if len(protocols) == 0 {
		return nil, fmt.Errorf("no protocols supplied")
	}

	if publishKey != "" {
		if !regexp.MustCompile("^[a-zA-Z0-9]+$").MatchString(publishKey) {
			return nil, fmt.Errorf("publish key must be alphanumeric")
		}
	}

	log.Printf("rtsp-simple-server %s", Version)

	p := &program{
		protocols:  protocols,
		rtspPort:   rtspPort,
		rtpPort:    rtpPort,
		rtcpPort:   rtcpPort,
		publishKey: publishKey,
		clients:    make(map[*client]struct{}),
		publishers: make(map[string]*client),
	}

	var err error

	p.rtpl, err = newRtspUdpListener(p, rtpPort, _TRACK_FLOW_RTP)
	if err != nil {
		return nil, err
	}

	p.rtcpl, err = newRtspUdpListener(p, rtcpPort, _TRACK_FLOW_RTCP)
	if err != nil {
		return nil, err
	}

	p.rtspl, err = newRtspTcpListener(p)
	if err != nil {
		return nil, err
	}

	return p, nil
}

func (p *program) run() {
	go p.rtpl.run()
	go p.rtcpl.run()
	go p.rtspl.run()

	infty := make(chan struct{})
	<-infty
}

func (p *program) forwardTrack(path string, id int, flow trackFlow, frame []byte) {
	for c := range p.clients {
		if c.path == path && c.state == _CLIENT_STATE_PLAY {
			if c.streamProtocol == _STREAM_PROTOCOL_UDP {
				if flow == _TRACK_FLOW_RTP {
					p.rtpl.nconn.WriteTo(frame, &net.UDPAddr{
						IP:   c.ip,
						Port: c.streamTracks[id].rtpPort,
					})
				} else {
					p.rtcpl.nconn.WriteTo(frame, &net.UDPAddr{
						IP:   c.ip,
						Port: c.streamTracks[id].rtcpPort,
					})
				}

			} else {
				c.rconn.WriteInterleavedFrame(trackToInterleavedChannel(id, flow), frame)
			}
		}
	}
}

func main() {
	kingpin.CommandLine.Help = "rtsp-simple-server " + Version + "\n\n" +
		"RTSP server."

	version := kingpin.Flag("version", "print rtsp-simple-server version").Bool()
	protocols := kingpin.Flag("protocols", "supported protocols").Default("udp,tcp").String()
	rtspPort := kingpin.Flag("rtsp-port", "port of the RTSP TCP listener").Default("8554").Int()
	rtpPort := kingpin.Flag("rtp-port", "port of the RTP UDP listener").Default("8000").Int()
	rtcpPort := kingpin.Flag("rtcp-port", "port of the RTCP UDP listener").Default("8001").Int()
	publishKey := kingpin.Flag("publish-key", "optional authentication key required to publish").Default("").String()

	kingpin.Parse()

	if *version == true {
		fmt.Println("rtsp-simple-server " + Version)
		os.Exit(0)
	}

	p, err := newProgram(*protocols, *rtspPort, *rtpPort, *rtcpPort, *publishKey)
	if err != nil {
		log.Fatal("ERR: ", err)
	}

	p.run()
}