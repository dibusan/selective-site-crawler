/*
	Crawl a host and save all relevant pages to local storage
*/
package main

import (
	"flag"
	"fmt"
	"os"
	"log"
	"encoding/hex"
	"crypto/rand"
	"net/url"
	"errors"
	"sync"
	"strings"
	"io/ioutil"
	"io"
	"net/http"
	"bytes"
	"golang.org/x/net/html"
	"time"
	"strconv"
)

// concurrentStorage acts as a set. A common storage point for multiple go routines and
// as a validator, to avoid processing urls that have already been processed by other routines.
type concurrentStorage struct {
	sync.Mutex
	domain string
	urls map[url.URL]bool
	urlsSize int
}

func newConcurrentStorage(d string) *concurrentStorage{
	return &concurrentStorage{
		domain: d,
		urls: map[url.URL]bool{},
	}
}

// Return true if the URL is unseen and was saved.
//
// add saves a URL iff it hasn't been processed by a go routine. If it
// cannot save it, then returns an empty URL and false to let the caller
// know not to process it.
func (c *concurrentStorage) add(u url.URL) (bool) {
	c.Lock()
	defer c.Unlock()
	if _, ok := c.urls[u]; ok{
		return false
	}
	c.urls[u] = true
	c.urlsSize++
	return true
}

func (c *concurrentStorage) size() int {
	c.Lock()
	defer c.Unlock()
	return c.urlsSize
}

const (
	ERROR	=  iota // 0
	WARNING			// 1
	INFO			// 2
	DEBUG			// 3
	VERBOSE			// 4
)

var (
	runtimeLog *os.File
	logger *log.Logger
	logLevel = ERROR
)

func randomHex(n int) (string, error) {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func initLogger(ll int){
	logLevel = ll
	runtimeLog, err := os.OpenFile("/var/log/scrapefreeproxylist.log",
		os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		fmt.Printf("error opening file: %v", err)
		os.Exit(1)
	}

	hex, _ := randomHex(10)
	prefix := hex + "-applog:"

	logger = log.New(runtimeLog, prefix, log.Lshortfile|log.LstdFlags)
	logger.Println("---------- START " + prefix + "----------")
	fmt.Println("---------- START " + prefix + "----------")
}

func logError(msg string){
	cMsg := "ERROR: " + msg
	logger.Println(cMsg)
	fmt.Println(cMsg)
}

func logWarning(msg string){
	if(logLevel >= WARNING){
		cMsg := "WARNING: " + msg
		logger.Println(cMsg)
		fmt.Println(cMsg)
	}
}

func logInfo(msg string){
	if(logLevel >= INFO){
		logger.Println("INFO: " + msg)
		fmt.Println("INFO: " + msg)
	}
}

func logDebug(msg string){
	if(logLevel >= DEBUG){
		logger.Println("DEBUG: " + msg)
		fmt.Println("DEBUG: " + msg)
	}
}

func logVerbose(msg string){
	if(logLevel == VERBOSE){
		logger.Println("VERBOSE: " + msg)
		fmt.Println("VERBOSE: " + msg)
	}
}

var (
	domain string
	timeout int
	pageLimit int
	pageCounter int
)



func validateUrl(u url.URL) error {
	if u.Host == "" {
		return errors.New("Try the format https://www.example.com. No host found in " +
			domain)
	}
	return nil
}


// Get the contents of a web page
// Return error if the request fails
func getHttp(url url.URL) (io.ReadCloser, error) {
	resp, err := http.Get(url.String())
	if err != nil {
		log.Printf("HTTP failed to GET url=%s. error=%s\n", url.String(), err)
		return nil, err
	}

	return resp.Body, nil
}

// Extract the href attribute from a Token
func getHref(t html.Token) (ok bool, href string) {
	for _, a := range t.Attr {
		if a.Key == "href" {
			href = a.Val
			ok = true
		}
	}
	return
}

// adds missing pieces to a URL and then validates it.
// if is an invalid/non-accessible URL then return false
func sanitizeUrl(href string, domain string) (url.URL, bool){
	if strings.Trim(href, " ") == ""{
		return url.URL{}, false
	}

	u, err := url.Parse(href)
	if err != nil {
		log.Println(err)
		return url.URL{}, false
	}

	if u.Host == ""{
		u.Host = domain
	} else if u.Host != domain || u.Path == "/" || u.Path == ""{
		return url.URL{}, false
	}

	if u.Scheme == ""{
		u.Scheme = "https"
	}

	// Ignore alien schemas [ mailto, ftp, etc ]
	if !strings.Contains(u.Scheme, "http") {
		return url.URL{}, false
	}

	// TODO: Check URL is accessible

	return *u, true
}

// Get only urls of the specified domain given the body of a web page
func getUrls(body []byte, domain string) ([]url.URL, error) {

	// holds only valid urls
	var urls []url.URL

	reader := bytes.NewReader(body)
	tokenizer := html.NewTokenizer(reader)

	infinitefor:for {
		tokenType := tokenizer.Next()

		switch {
		case tokenType == html.ErrorToken:
			// End of the document, we're done
			break infinitefor

		case tokenType == html.StartTagToken:
			t := tokenizer.Token()

			// Check if the token is an <a> tag
			isAnchor := t.Data == "a"
			if !isAnchor {
				continue
			}

			// Extract the href value, if there is one
			ok, href := getHref(t)
			if !ok {
				continue
			}

			if url, ok := sanitizeUrl(href, domain); ok {
				urls = append(urls, url)
			}
		}
	}
	return urls, nil
}

// Save the page contents (converted to a byte array) to a file in local storage
// Returns whether the page was saved successfully
func savePage(url url.URL, body []byte) bool{
	// TODO: Take save location as a CMD line flag
	rootDir := "/tmp/scraper"

	dirPath := rootDir + "/" + url.Host + url.Path

	err := os.MkdirAll(dirPath, 0777)
	if err != nil {
		log.Printf("Cannot create directory %s. \nError: %s", dirPath, err)
		return false
	}

	filePath := dirPath + "/index.html"

	err = ioutil.WriteFile(filePath, body, 0777)
	if err != nil {
		log.Printf("Cannot write to file=%s. \nError: %s", filePath, err)
		return false
	}
	return true
}

// scrape visits a page and extracts all the valid urls for the given domain
// Returns error if the target URL is empty, cannot be scrapped by access over HTTP,
// urls cannot be scraped.
func scrape(u url.URL) ([]url.URL, error) {

	if strings.Trim(u.String(), " ") == ""{
		return []url.URL{}, errors.New("empty url")
	}

	pageReadCloser, err := getHttp(u)
	defer pageReadCloser.Close()
	if err != nil {
		log.Printf("failed to get pageReadCloser at u=%s. err=%s\n", u, err)
		return []url.URL{}, nil
	}

	page, err := ioutil.ReadAll(pageReadCloser)
	if err != nil {
		log.Printf("Could not read page buffer for url=%s\n", u.String())
		return []url.URL{}, err
	}

	if savePage(u, page) {
		pageCounter++
	}

	if pageLimit != -1 && pageCounter >= pageLimit {
		logInfo("Reached page download limit=" + strconv.Itoa(pageLimit))
		os.Exit(0)
	}

	urls, err := getUrls(page, u.Host)
	if err != nil {
		log.Printf("failed to extract valid urls for pageReadCloser at u=%s. err=%s\n", u, err)
		return []url.URL{}, err
	}

	return urls, nil
}

// crawl could be called multiple times in parallel to increase productivity.
func crawl(urlSet *concurrentStorage, ch chan url.URL){
	for {
		select {
		case u := <- ch:
			if ok := urlSet.add(u); ok {
				log.Printf("Received url=%s", u.String())
				urls, err := scrape(u)
				if err != nil {
					log.Printf("Could not scrape url=%s.\nError: %s", u.String(), err)
					break
				}

				for _, url := range urls {
					go 	func() {ch <- url}()
				}
			}
		}
	}
}

// todo: unittest
func validateFlags(d string, t int, p int) error{
	if d == "" {
		eMsg :=  "-host needs to be set"
		//logError(eMsg)
		return errors.New(eMsg)
	}

	if t == -1 && p == -1 {
		eMsg := "-timeout or -pages needs to be set"
		//logError(eMsg)
		return errors.New(eMsg)
	}
	// todo: validate flags
	return nil
}

func main() {
	initLogger(VERBOSE)
	pageCounter = 0

	flag.StringVar(&domain, "host", "", "The url to scrape.")
	flag.IntVar(&timeout, "timeout", -1, "Lifetime of this " +
		"process (in seconds). If not set will run indefinitely until another " +
		"constraint is met (page limit). At least one constraint needs to be " +
		"set.")
	flag.IntVar(&pageLimit, "pages", -1, "Limit of pages to" +
		" visit. If not set will run until the timeout constraint is met. At " +
		"least one constraint needs to be set.")
	flag.Parse()

	err := validateFlags(domain, timeout, pageLimit)
	if err != nil {
		logError("Invalid flags. Err: " + err.Error())
		os.Exit(1)
	}

	targetURL, err:= url.Parse(domain)
	if err != nil {
		logError("Could not parse target url: " + domain)
		logError("Err: " + err.Error())
		os.Exit(1)
	}

	err = validateUrl(*targetURL)
	if err != nil {
		logError("Failed to parse url. Err: " + err.Error())
		os.Exit(1)
	}

	// TODO: write function to find a valid schema by requesting with multiple versions of the url
	if targetURL.Scheme == "" {
		targetURL.Scheme = "https"
	}

	urlSet := newConcurrentStorage(targetURL.Host)

	urlCh := make(chan url.URL, 2)
	go crawl(urlSet, urlCh)
	go crawl(urlSet, urlCh)

	urlCh <- *targetURL

	if timeout != -1 {
		time.Sleep(time.Duration(timeout) * time.Second)
	} else {
		time.Sleep(time.Duration(1) * time.Hour) // Max time
	}
}
