package publisher

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Sirupsen/logrus"
	"github.com/honeycombio/honeytail/event"
	"github.com/honeycombio/honeytail/parsers/mysql"
	"github.com/honeycombio/libhoney-go"
)

// Publisher is an interface to write rdslogs entries to a target. Current
// implementations are STDOUT and Honeycomb
type Publisher interface {
	Write(line string)
}

// HoneycombPublisher implements Publisher and sends the entries provided to
// Honeycomb
type HoneycombPublisher struct {
	Writekey     string
	Dataset      string
	APIHost      string
	ScrubQuery   bool
	SampleRate   int
	initialized  bool
	mysqlParser  *mysql.Parser
	lines        chan string
	eventsToSend chan event.Event
}

func (h *HoneycombPublisher) Write(chunk string) {
	if !h.initialized {
		fmt.Fprintln(os.Stderr, "initializing honeycomb")
		h.initialized = true
		libhoney.Init(libhoney.Config{
			WriteKey:   h.Writekey,
			Dataset:    h.Dataset,
			APIHost:    h.APIHost,
			SampleRate: uint(h.SampleRate),
		})
		h.mysqlParser = &mysql.Parser{
			SampleRate: h.SampleRate,
		}
		h.mysqlParser.Init(&mysql.Options{})
		h.lines = make(chan string)
		h.eventsToSend = make(chan event.Event)
		go func() {
			h.mysqlParser.ProcessLines(h.lines, h.eventsToSend, nil)
			close(h.eventsToSend)
		}()
		go func() {
			fmt.Fprintln(os.Stderr, "spinning up goroutine to send events")
			for ev := range h.eventsToSend {
				if h.ScrubQuery {
					if val, ok := ev.Data["query"]; ok {
						// generate a sha256 hash
						newVal := sha256.Sum256([]byte(fmt.Sprintf("%v", val)))
						// and use the base16 string version of it
						ev.Data["query"] = fmt.Sprintf("%x", newVal)
					}
				}
				libhEv := libhoney.NewEvent()
				libhEv.Timestamp = ev.Timestamp
				if err := libhEv.Add(ev.Data); err != nil {
					logrus.WithFields(logrus.Fields{
						"event": ev,
						"error": err,
					}).Error("Unexpected error adding data to libhoney event")
				}
				// sampling is handled by the mysql parser
				if err := libhEv.SendPresampled(); err != nil {
					logrus.WithFields(logrus.Fields{
						"event": ev,
						"error": err,
					}).Error("Unexpected error event to libhoney send")
				}

			}
		}()
	}
	lines := strings.Split(chunk, "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		h.lines <- line
	}
}

// STDOUTPublisher implements Publisher and sends the entries provided to
// Honeycomb
type STDOUTPublisher struct {
}

func (s *STDOUTPublisher) Write(line string) {
	io.WriteString(os.Stdout, line)
}
