package alert

import (
	"bytes"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/influxdata/influxdb/influxql"
	"github.com/influxdata/kapacitor/bufpool"
	"github.com/influxdata/kapacitor/command"
)

// AlertData is a structure that contains relevant data about an alert event.
// The structure is intended to be JSON encoded, providing a consistent data format.
type AlertData struct {
	ID       string          `json:"id"`
	Message  string          `json:"message"`
	Details  string          `json:"details"`
	Time     time.Time       `json:"time"`
	Duration time.Duration   `json:"duration"`
	Level    Level           `json:"level"`
	Data     influxql.Result `json:"data"`
}

func alertDataFromEvent(event Event) AlertData {
	return AlertData{
		ID:       event.State.ID,
		Message:  event.State.Message,
		Details:  event.State.Details,
		Time:     event.State.Time,
		Duration: event.State.Duration,
		Level:    event.State.Level,
		Data:     event.Data.Result,
	}
}

// Default log mode for file
const defaultLogFileMode = 0600

type LogHandlerConfig struct {
	Path string
	Mode os.FileMode
}

type logHandler struct {
	logpath string
	mode    os.FileMode
	logger  *log.Logger
}

func NewLogHandler(c LogHandlerConfig, l *log.Logger) Handler {
	return &logHandler{
		logpath: c.Path,
		mode:    c.Mode,
		logger:  l,
	}
}

func (h *logHandler) Handle(event Event) {
	ad := alertDataFromEvent(event)

	f, err := os.OpenFile(h.logpath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, os.FileMode(h.mode))
	if err != nil {
		h.logger.Printf("E! failed to open file %s for alert logging: %v", h.logpath, err)
		return
	}
	defer f.Close()

	err = json.NewEncoder(f).Encode(ad)
	if err != nil {
		h.logger.Printf("E! failed to marshal alert data json: %v", err)
	}
}

type ExecHandlerConfig struct {
	Prog      string
	Args      []string
	Commander command.Commander
}

type execHandler struct {
	bp        *bufpool.Pool
	ci        command.CommandInfo
	commander command.Commander
	logger    *log.Logger
}

func NewExecHandler(c ExecHandlerConfig, l *log.Logger) Handler {
	ci := command.CommandInfo{
		Prog: c.Prog,
		Args: c.Args,
	}
	return &execHandler{
		bp:        bufpool.New(),
		ci:        ci,
		commander: c.Commander,
		logger:    l,
	}
}

func (h *execHandler) Handle(event Event) {
	buf := h.bp.Get()
	defer h.bp.Put(buf)
	ad := alertDataFromEvent(event)

	err := json.NewEncoder(buf).Encode(ad)
	if err != nil {
		h.logger.Printf("E! failed to marshal alert data json: %v", err)
		return
	}

	cmd := h.commander.NewCommand(h.ci)
	cmd.Stdin(buf)
	var out bytes.Buffer
	cmd.Stdout(&out)
	cmd.Stderr(&out)
	err = cmd.Start()
	if err != nil {
		h.logger.Printf("E! exec command failed: Output: %s: %v", out.String(), err)
		return
	}
	err = cmd.Wait()
	if err != nil {
		h.logger.Printf("E! exec command failed: Output: %s: %v", out.String(), err)
		return
	}
}

type TCPHandlerConfig struct {
	Addr string
}

type tcpHandler struct {
	bp     *bufpool.Pool
	addr   string
	logger *log.Logger
}

func NewTCPHandler(c TCPHandlerConfig, l *log.Logger) Handler {
	return &tcpHandler{
		bp:     bufpool.New(),
		addr:   c.Addr,
		logger: l,
	}
}

func (h *tcpHandler) Handle(event Event) {
	buf := h.bp.Get()
	defer h.bp.Put(buf)
	ad := alertDataFromEvent(event)

	err := json.NewEncoder(buf).Encode(ad)
	if err != nil {
		h.logger.Printf("E! failed to marshal alert data json: %v", err)
		return
	}

	conn, err := net.Dial("tcp", h.addr)
	if err != nil {
		h.logger.Printf("E! failed to connect to %s: %v", h.addr, err)
		return
	}
	defer conn.Close()

	buf.WriteByte('\n')
	conn.Write(buf.Bytes())
}

type PostHandlerConfig struct {
	URL string
}

type postHandler struct {
	bp     *bufpool.Pool
	url    string
	logger *log.Logger
}

func NewPostHandler(c PostHandlerConfig, l *log.Logger) Handler {
	return &postHandler{
		bp:     bufpool.New(),
		url:    c.URL,
		logger: l,
	}
}

func (h *postHandler) Handle(event Event) {
	body := h.bp.Get()
	defer h.bp.Put(body)
	ad := alertDataFromEvent(event)

	err := json.NewEncoder(body).Encode(ad)
	if err != nil {
		h.logger.Printf("E! failed to marshal alert data json: %v", err)
		return
	}

	resp, err := http.Post(h.url, "application/json", body)
	if err != nil {
		h.logger.Printf("E! failed to POST alert data: %v", err)
		return
	}
	resp.Body.Close()
}
