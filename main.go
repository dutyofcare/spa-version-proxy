package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const EnvVarPrefix = "SPA_PROXY_"

func main() {
	var err error
	sourceURLString := os.Getenv(EnvVarPrefix + "SOURCE")

	var handler http.Handler
	sourceClient := &http.Client{
		Timeout: time.Second * 10,
	}

	var defaultVersion StringReader
	if specifiedDefaultVersion := os.Getenv(EnvVarPrefix + "DEFAULT_VERSION"); specifiedDefaultVersion != "" {
		defaultVersion = normalStringReader(specifiedDefaultVersion)
	} else {
		defaultVersion, err = defaultVersionPoller(sourceClient, sourceURLString+"/default-version.txt")
		if err != nil {
			log.Fatalf("Fetching default version: %s", err.Error())
		}
	}

	sourceURL, err := url.Parse(sourceURLString)
	if err != nil {
		log.Fatalf("Invalid url in $%sSOURCE: %s", EnvVarPrefix, err.Error())
	}

	cacheDir := os.Getenv(EnvVarPrefix + "CACHE_DIR")
	handler = fileServer{
		root:      httpDir{Dir: http.Dir(cacheDir)},
		sourceURL: sourceURL,
		client:    sourceClient,
	}

	handler = VersionSwitch(defaultVersion)(handler)
	handler = AppRewrite(handler)
	handler = Logger(handler)

	if proxyConfigFile := os.Getenv(EnvVarPrefix + "DEV_PATHS"); proxyConfigFile != "" {
		proxyConfig := []ProxyConfig{}
		if err := loadJSONFile(proxyConfigFile, &proxyConfig); err != nil {
			log.Fatalf("Loading Proxy Config %s", err.Error())
		}
		handler = ProxyPaths(proxyConfig)(handler)
	}

	bindAddress := os.Getenv(EnvVarPrefix + "BIND")
	if err := http.ListenAndServe(bindAddress, handler); err != nil {
		log.Fatal(err.Error())
	}
}

type threadSafeString struct {
	mutex sync.RWMutex
	value string
}

// Write sets the value, and returns true if it changed.
func (tss *threadSafeString) Write(val string) bool {
	tss.mutex.Lock()
	defer tss.mutex.Unlock()
	if tss.value == val {
		return false
	}
	tss.value = val
	return true
}

func (tss *threadSafeString) Read() string {
	tss.mutex.RLock()
	defer tss.mutex.RUnlock()
	return tss.value
}

type normalStringReader string

func (str normalStringReader) Read() string {
	return string(str)
}

type StringReader interface {
	Read() string
}

func defaultVersionPoller(client *http.Client, url string) (StringReader, error) {

	fetchVersion := func() (string, error) {
		res, err := client.Get(url)
		if err != nil {
			return "", err
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			return "", fmt.Errorf("HTTP %s getting version", res.Status)
		}
		versionBytes, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(versionBytes)), nil
	}

	defaultVersion, err := fetchVersion()
	if err != nil {
		return nil, err
	}
	log.Printf("Default Version from source: '%s'", defaultVersion)

	versionString := &threadSafeString{
		value: defaultVersion,
	}

	go func() {
		for {
			newVersion, err := fetchVersion()
			if err != nil {
				log.Println(err.Error())
				time.Sleep(time.Second * 5)
				continue
			}

			changed := versionString.Write(newVersion)
			if changed {
				log.Printf("Updating default version to '%s'", newVersion)
			}
			time.Sleep(time.Minute)
		}
	}()

	return versionString, nil
}

type httpDir struct {
	http.Dir
}

func (d httpDir) Create(name string) (*os.File, error) {
	// This function is a clone of the Open function in http.Dir, but for
	// creating rather than opening read-only
	// Begin Direct Copy
	if filepath.Separator != '/' && strings.ContainsRune(name, filepath.Separator) {
		return nil, errors.New("http: invalid character in file path")
	}
	dir := string(d.Dir)
	if dir == "" {
		dir = "."
	}

	fullName := filepath.Join(dir, filepath.FromSlash(path.Clean("/"+name)))
	// End Direct Copy

	os.MkdirAll(filepath.Dir(fullName), os.ModePerm)
	return os.Create(fullName)
}

type fileServer struct {
	root      httpDir
	sourceURL *url.URL
	client    *http.Client
}

func (fs fileServer) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	rw.Header().Set("X-Cache", "hit")
	req.URL.Path = path.Clean(req.URL.Path)
	err := fs.tryServeFile(rw, req)
	if os.IsNotExist(err) {
		rw.Header().Set("X-Cache", "miss")
		if err := fs.doCacheFetch(rw, req); err != nil {
			doError(rw, req, err)
			return
		}
		if err := fs.tryServeFile(rw, req); err != nil {
			doError(rw, req, err)
			return
		}
	} else if err != nil {
		doError(rw, req, err)
		return
	}
}

func (fs fileServer) doCacheFetch(rw http.ResponseWriter, req *http.Request) error {
	// TODO: Exclusive Lock - Will multiple concurrent fetches corrupt the file
	// or error out?

	urlOut := &url.URL{
		Path:   path.Join(fs.sourceURL.Path, req.URL.Path),
		Scheme: fs.sourceURL.Scheme,
		Host:   fs.sourceURL.Host,
	}

	res, err := fs.client.Get(urlOut.String())
	if err != nil {
		return err
	}

	cacheFile, err := fs.root.Create(req.URL.Path)
	if err != nil {
		return err
	}
	defer cacheFile.Close()

	return res.Write(cacheFile)

}

func (fs fileServer) tryServeFile(rw http.ResponseWriter, req *http.Request) error {
	// http.Dir.Open ensures the file is rooted at root.
	f, err := fs.root.Open(req.URL.Path)
	if err != nil {
		return err
	}
	defer f.Close()

	buffered := bufio.NewReader(f)
	parsedResponse, err := http.ReadResponse(buffered, nil)
	if err != nil {
		return err
	}
	defer parsedResponse.Body.Close()

	// TODO: Discard and delete if cache is expired.

	copyHeaders(parsedResponse.Header, rw.Header())
	rw.WriteHeader(parsedResponse.StatusCode)
	_, err = io.Copy(rw, parsedResponse.Body)
	return err
}

const VersionCookieName = "version-override"

// VersionSwitch rewrites requests to a directory prefixed with the requested
// or default version.  The version can be set with a querystirng version= or
// cookie. When the querystring parameter is set, the cookie is sent with the
// response so that requests for resources in HTML pages (css, images etc) will
// also get the correct prefix.
func VersionSwitch(defaultVersion StringReader) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {

			var version string
			if queryVersion := req.URL.Query().Get("version"); queryVersion != "" {
				// read the requested version from the QS
				version = queryVersion

				// Set a cookie so that dependencies are also loaded with the
				// correct version
				versionCookie := &http.Cookie{
					Name: VersionCookieName,
					// Allowing JS code to view and modify could extend
					// functionality.
					HttpOnly: false,
					Path:     "/",
					Expires:  time.Now().Add(time.Hour),
					Value:    version,
				}
				http.SetCookie(rw, versionCookie)

				// Don't cacne versioned entry points
				rw.Header().Set("Cache-Control", "no-store")
			} else if versionCookie, _ := req.Cookie(VersionCookieName); versionCookie != nil {
				// read the requested version from the cookie
				version = versionCookie.Value

				// refresh the cookie
				versionCookie.Expires = time.Now().Add(time.Hour)
				http.SetCookie(rw, versionCookie)

				// Don't cache versioned resources (Cookies are not considered
				// by browsers when looking up cached responses)
				rw.Header().Set("Cache-Control", "no-store")
			} else {
				version = defaultVersion.Read()
			}

			version = url.PathEscape(version)
			newPath := path.Clean("/" + path.Join(version, req.URL.Path))
			req.URL.Path = newPath
			next.ServeHTTP(rw, req)
		})
	}
}

// AppRewrite rewrites all requests without an extension to /index.html
func AppRewrite(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if ext := path.Ext(req.URL.Path); ext == "" {
			req.URL.Path = "/index.html"
		}
		next.ServeHTTP(rw, req)
	})
}

func copyHeaders(from, to http.Header) {
	for headerName, headerValues := range from {
		for _, headerValue := range headerValues {
			to.Add(headerName, headerValue)
		}
	}
}

func doError(rw http.ResponseWriter, req *http.Request, err error) {
	log.Printf("ERROR: %s", err.Error())
	rw.WriteHeader(500)
}

func loadJSONFile(filename string, into interface{}) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewDecoder(f).Decode(into)
}
