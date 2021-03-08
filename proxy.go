package main

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

//websocket header is {"Upgrade": "websocket"} {"Connection": "Upgrade"}
var cache map[string]http.Response
var storage *Storage
var blacklist map[string]bool
var cacheDuration string

//Setup the various global required variables
func setup() {
	blacklist = make(map[string]bool)
	cache = make(map[string]http.Response)
	storage = NewStorage()
	cacheDuration = "10s"
}

/*Read from the management console and parse the following commands:
- block <URL> -> Add a particular base host to the blacklist
- unblock <URL> -> Remove a particular base host from the blacklist, or add it to the map as unblocked
- lblock -> List all items currently stored in the blacklist
- l -> List all items currently stored in the cache
*/
func readConsole() {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("-> ")
		text, err := reader.ReadString('\n')
		if err != nil {
			log.Panic(err)
		}
		if text != "\n" {
			parseCommand(text)
		}
	}
}

func parseCommand(input string) {
	input = strings.ToLower(input)
	command := strings.Fields(input)
	if command[0] == "block" {
		blacklist[command[1]] = true
		log.Println(command[1], "added to blocked URI list")
		log.Println(blacklist)
	} else if command[0] == "unblock" {
		blacklist[command[1]] = false
		log.Println(command[1], "removed from blocked URL list")
	} else if command[0] == "lblock" {
		log.Println("\u001b[31m", blacklist, "\u001b[0m")
	} else if command[0] == "l" {
		log.Println("\u001b[32m", storage.items, "\u001b[0m")
	}
}

/*
General connection handler function:
This function decides how to handle the browsers requests.

If the request is to a blocked URL it gets blocked and closes the connection
If the request is a HTTPS CONNECT it will be passed to the httphandler function
If the request is not a HTTPS CONNECT it will be passed to the http handler function

*/
func connectionHandler(writer http.ResponseWriter, request *http.Request) {
	requestURL := ""
	if request.Method == http.MethodConnect {
		requestURL = strings.Split(request.RequestURI, ":")[0]
	} else {
		requestURL = strings.Split(request.RequestURI, "/")[2]
	}

	requestBaseHost := stripSubDomains(requestURL)

	log.Println("Request received", request.Method, request.RequestURI, requestURL, "with base", requestBaseHost)

	//Check if its blacklisted, if yes then abort
	if val, ok := blacklist[requestBaseHost]; ok {
		if val {
			log.Println(requestURL, "has been blocked")
			writer.WriteHeader(http.StatusForbidden)
			request.Body.Close()
			return
		}
	}

	if request.Method == http.MethodConnect {
		handleHTTPS(writer, request)
	} else {
		handleHTTP(writer, request)
	}
}

//stripSubDomains reduces the request URI down to its base so that it can be checked against the blocked URLs
func stripSubDomains(reqURL string) string {
	requestURL := strings.Split(reqURL, ":")[0]
	dotre := regexp.MustCompile(`\.`)
	dotIndexArr := dotre.FindAllStringIndex(requestURL, -1)
	idxBase := 0
	requestBaseHost := ""
	if len(dotIndexArr) > 1 {
		idxBase = dotIndexArr[len(dotIndexArr)-2][1]
		requestBaseHost = requestURL[idxBase:]
	}
	if requestBaseHost == "" {
		requestBaseHost = requestURL
	}
	return requestBaseHost
}

/*Handle HTTP handles HTTP requests.
- 	HTTP requests will either be fullfilled by the cache or by a
	request to the server.
- 	Should a cache miss occur then the server response will be used
	to populate that cache line.

*/
func handleHTTP(writer http.ResponseWriter, request *http.Request) {
	defer request.Body.Close()
	response, prs := storage.Get(request.RequestURI)

	client := &http.Client{}
	var err error
	if prs == true && request.Method == http.MethodGet {
		request.RequestURI = ""
		writer.Header().Add("cached", "1")

	} else {
		key := request.RequestURI
		request.RequestURI = ""
		unCached, err := client.Do(request)
		if err != nil {
			log.Fatal(err)
		}
		if t, err := time.ParseDuration(cacheDuration); err == nil {
			storage.Store(key, *unCached, t)
		} else {
			log.Panicln("Cache error", err)
		}
		response = unCached
		log.Println("New page cached", key)

	}

	for k, v := range response.Header {
		for _, vv := range v {
			writer.Header().Add(k, vv)
		}
	}
	writer.WriteHeader(response.StatusCode)
	result, err := ioutil.ReadAll(response.Body)
	if err != nil && err != io.EOF {
		log.Panic(err)
	}
	writer.Write(result)
}

/* Handle HTTPS will
- 	 Create a TCP tunnel to create a HTTPS tunnel
-    Copy headers between the client and server

*/

func handleHTTPS(writer http.ResponseWriter, request *http.Request) {
	destination, err := net.DialTimeout("tcp", request.Host, 10*time.Second)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusServiceUnavailable)
		log.Println("HTTPS connection timeout:", err)
		return
	}
	writer.WriteHeader(http.StatusOK)

	hijacker, ok := writer.(http.Hijacker)
	if !ok {
		http.Error(writer, "Hijacking not supported", http.StatusInternalServerError)
		log.Println("Hijacking unsupported", err)
		return
	}
	clientConnection, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(writer, err.Error(), http.StatusServiceUnavailable)
	}

	go copyHeaders(destination, clientConnection)
	go copyHeaders(clientConnection, destination)

}

func copyHeaders(dest io.WriteCloser, source io.ReadCloser) {
	defer dest.Close()
	defer source.Close()
	io.Copy(dest, source)
}

func main() {
	setup()
	httpHandler := http.HandlerFunc(connectionHandler)
	go readConsole()
	http.ListenAndServe(":8080", httpHandler)
}
