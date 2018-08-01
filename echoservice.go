package echoservice

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/JeremyOT/httpserver"
	"github.com/gorilla/websocket"
)

const (
	// HeaderExpectStatus allows you to specify the status code to send in
	// the response.
	HeaderExpectStatus = "Expect-Status"

	// HeaderExpectHeaders allows you to specify a string:string map of headers
	// to send in the response.
	HeaderExpectHeaders = "Expect-Headers"

	// HeaderExpectChunked allows you to specify that the response be sent using
	// chunked encoding.
	HeaderExpectChunked = "Expect-Chunked"
)

var upgrader = websocket.Upgrader{}

// Service is a simple HTTP server for use in tests that sends a specific
// response based on the request.
type Service struct {
	*httpserver.Server
	// RequestLogger is called on each request for logging purposes
	RequestLogger func(req *http.Request)
}

// Body represents the JSON encoded echo response.
type Body struct {
	Method  string `json:"method"`
	Path    string `json:"path"`
	URL     string `json:"url"`
	Host    string `json:"host"`
	Request string `json:"request"`
}

// ReadBody is a convenience method for parsing a response body
func ReadBody(r io.Reader) *Body {
	var body Body
	err := json.NewDecoder(r).Decode(&body)
	if err != nil {
		log.Println("Invalid echo body:", err)
		return nil
	}
	return &body
}

// ReadBody is a convenience method for parsing a response body and closing the
// stream.
func ReadAllBody(r io.ReadCloser) *Body {
	body := ReadBody(r)
	r.Close()
	return body
}

func (s *Service) handleWebsocket(writer http.ResponseWriter, request *http.Request) {
	conn, err := upgrader.Upgrade(writer, request, nil)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
	defer conn.Close()
	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			log.Println("Socket error:", err)
			return
		}
		err = conn.WriteMessage(messageType, data)
		if err != nil {
			log.Println("Socket error:", err)
			return
		}
	}
}

func (s *Service) handleRequest(writer http.ResponseWriter, request *http.Request) {
	if s.RequestLogger != nil {
		s.RequestLogger(request)
	}
	if request.Header.Get("Connection") == "Upgrade" && strings.Contains(request.Header.Get("Upgrade"), "websocket") {
		log.Printf("Req: %+v", request.URL)
		s.handleWebsocket(writer, request)
		return
	}
	var buffer bytes.Buffer
	request.Write(&buffer)
	body := Body{
		Method:  request.Method,
		Path:    request.URL.Path,
		URL:     request.URL.String(),
		Host:    request.Host,
		Request: string(buffer.Bytes()),
	}
	expectedStatus := request.Header.Get(HeaderExpectStatus)
	expectedHeaders := request.Header.Get(HeaderExpectHeaders)
	expectChunked := request.Header.Get(HeaderExpectChunked)
	writer.Header().Set("Content-Type", "application/json")
	if expectedHeaders != "" {
		var headers map[string]string
		if err := json.Unmarshal([]byte(expectedHeaders), &headers); err != nil {
			log.Println("Error parsing Expect-Headers:", err)
		} else {
			for k, v := range headers {
				writer.Header().Set(k, v)
			}
		}
	}
	if expectedStatus != "" {
		if status, err := strconv.Atoi(expectedStatus); err != nil {
			log.Println("Error parsing Expect-Status:", err)
		} else {
			writer.WriteHeader(status)
			if status == http.StatusNoContent {
				return
			}
		}
	}
	if expectChunked != "" {
		flusher, ok := writer.(http.Flusher)
		if !ok {
			http.Error(writer, "Cannot send chunked response", http.StatusInternalServerError)
			return
		}
		json.NewEncoder(writer).Encode(&body)
		flusher.Flush()
	} else {
		json.NewEncoder(writer).Encode(&body)
	}
}

// NewService returns a new echo service.
func NewService() *Service {
	httpMux := http.NewServeMux()
	s := &Service{}
	httpMux.HandleFunc("/", s.handleRequest)
	s.Server = httpserver.New(httpMux.ServeHTTP)
	return s
}
