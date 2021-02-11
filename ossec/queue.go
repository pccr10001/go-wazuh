package ossec

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"
)

type WithLoggin struct {
}

// Queue helper to create a custom wodle
type Queue struct {
	TargetQueue rune
	AgentName   string
	Type        string
	QueuePath   string
	InitInfo    *InitInfo
	Logger      *zap.Logger
	ctx         context.Context
}

var (
	fieldDebugName = "Wodle event"
)

// QueueOption allows setting custom parameters during construction
type QueueOption func(*Queue) error

// WithTargetQueue use a custom target queue
func WithTargetQueue(queue rune) QueueOption {
	return func(w *Queue) error {
		w.TargetQueue = queue
		return nil
	}
}

// WithQueuePath use a custom queue path
func WithQueuePath(path string) QueueOption {
	return func(w *Queue) error {
		w.QueuePath = path
		return nil
	}
}

// WithInitInfo use a custom context
func WithInitInfo(initInfo *InitInfo) QueueOption {
	return func(w *Queue) error {
		w.InitInfo = initInfo
		return nil
	}
}

// WithQueueLogger use a custom logger
func WithQueueLogger(logger *zap.Logger) QueueOption {
	return func(w *Queue) error {
		w.Logger = logger
		return nil
	}
}

// NewQueue create new wodle
func NewQueue(typ string, opts ...QueueOption) (*Queue, error) {

	w := &Queue{
		TargetQueue: LOCALFILE_MQ,
		Type:        typ,
	}

	// mutate agent and add all optional params
	for _, o := range opts {
		if err := o(w); err != nil {
			return nil, err
		}
	}

	//
	if w.ctx == nil {
		w.ctx = context.Background()
	}

	if w.QueuePath == "" {
		if w.InitInfo == nil {
			info, err := NewInitInfo()
			if err != nil {
				return nil, err
			}
			w.InitInfo = info
		}
		w.QueuePath = fmt.Sprintf("%s/queue/ossec/queue", w.InitInfo.Directory)
	}

	if w.AgentName == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return nil, err
		}
		w.AgentName = hostname

	}

	return w, nil
}

type wodleDebugEvent struct {
	Event string `json:"Wodle event"`
}

// IntegrationMeta standard metadata
type IntegrationMeta struct {
	//	InputType       string      `json:"input>type,omitempty"`
	//	DecoderName     string      `json:"decoder>name,omitempty"`
	ID               string      `json:"id,omitempty"`
	URL              string      `json:"url,omitempty"`
	User             string      `json:"user,omitempty"`
	SourceUser       string      `json:"srcuser,omitempty"`
	SourceIP         *net.IP     `json:"srcip,omitempty"`
	SourcePort       *uint       `json:"srcport,omitempty"`
	DestinationIP    *net.IP     `json:"dstip,omitempty"`
	DestinationGeoIP string      `json:"dstgeoip,omitempty"`
	DestinationUser  string      `json:"dstuser,omitempty"`
	DestinationPort  *uint       `json:"dstport,omitempty"`
	Protocol         string      `json:"protocol,omitempty"`
	Action           string      `json:"action,omitempty"`
	Status           string      `json:"status,omitempty"`
	SystemName       string      `json:"systemname,omitempty"`
	Data             interface{} `json:"webhook,omitempty"`
	ExtraData        interface{} `json:"extra_data,omitempty"`
}

// IntegrationEvent basic integration message
type IntegrationEvent struct {
	Integration string          `json:"integration,omitempty"`
	Meta        IntegrationMeta `json:"meta,omitempty"`
}

// Event static structured event data
// user, srcip, dstip, srcport, dstport, protocol, action, id, url, data, extra_data, status, system_name
// https://documentation.wazuh.com/4.0/user-manual/ruleset/dynamic-fields.html
// https://github.com/wazuh/wazuh/blob/master/src/analysisd/decoders/plugins/json_decoder.c
type Event struct {
	IntegrationEvent
	Event string `json:"Wodle event,omitempty"`
}

// QueuePosting a massage for the queue
type QueuePosting struct {
	Location    string
	ProgramName string
	Raw         interface{}
}

// DebugMessage send a debug event
func (w *Queue) DebugMessage(msg string) error {
	return w.sendMessage(Event{Event: msg}, "ossec", "wodle")
}

// SendMessage send a single message to the agent´s queue
func (w *Queue) SendMessage(event interface{}, location string, programName string) error {

	return w.sendMessage(event, location, programName)
}

func (w *Queue) sendMessage(event interface{}, location string, programName string) error {
	b, err := json.Marshal(event)
	if err != nil {
		return err
	}

	s, e := net.Dial("unixgram", w.QueuePath)
	if e != nil {
		return e
	}
	defer s.Close()

	wireMsg := fmt.Sprintf("%c:%s:%s %s %s:%s", w.TargetQueue, location, time.Now().UTC().Format("Jan 02 15:04:05"), w.AgentName, programName, string(b))
	_, err = s.Write([]byte(wireMsg))
	if e != nil {
		return e
	}
	if w.Logger != nil {
		w.Logger.Debug("Sent Message", zap.String("queue", fmt.Sprintf("%c", w.TargetQueue)), zap.String("location", location), zap.String("programName", programName), zap.Any("event", event))
	}
	return nil
}

// AgentLoop process incoming messages
func (w *Queue) AgentLoop() (chan *QueuePosting, chan error) {
	// make the context cancable
	input := make(chan *QueuePosting, 100)
	out := make(chan error)
	go func() {
		for {
			for msg := range input {
				if w.Logger != nil {
					w.Logger.Debug("queue entry", zap.Any("msg", msg))
				}
				var location, programName string
				location = msg.Location
				if location == "" {
					location = "ossecv"

				}
				programName = msg.ProgramName
				if location == "" {
					programName = filepath.Base(os.Args[0])
				}

				err := w.SendMessage(msg.Raw, location, programName)
				if err != nil {
					close(input)
					out <- err
					return
				}
			}
		}
	}()

	return input, out
}
