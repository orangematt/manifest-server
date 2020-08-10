// (c) Copyright 2017-2020 Matt Messier

package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	readTimeout  = 3 * time.Second
	writeTimeout = 3 * time.Second
)

type WebContentFunc func(http.ResponseWriter, *http.Request)

type WebContent struct {
	Func        WebContentFunc
	Content     []byte
	ContentType string
	ModifyTime  time.Time
}

type WebServer struct {
	httpServer  *http.Server
	httpsServer *http.Server
	wg          sync.WaitGroup

	certFile string
	keyFile  string

	lock    sync.Mutex
	content map[string]WebContent
}

func NewWebServer(
	httpAddress, httpsAddress, certFile, keyFile string,
) (*WebServer, error) {
	s := &WebServer{
		certFile: certFile,
		keyFile:  keyFile,
		content:  make(map[string]WebContent),
	}
	if s.keyFile == "" {
		s.keyFile = s.certFile
	}
	if httpAddress == "" {
		httpAddress = ":http"
	}
	if httpsAddress == "" {
		httpsAddress = ":https"
	}

	if certFile != "" {
		// Redirect HTTP requests to HTTPS
		s.httpServer = &http.Server{
			Handler: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Header().Set("Connection", "close")
				// FIXME: this should resolve httpsAddress if it's not default
				url := fmt.Sprintf("https://%s%s", req.Host, req.URL)
				http.Redirect(w, req, url, http.StatusMovedPermanently)
			}),
			Addr:         httpAddress,
			ReadTimeout:  readTimeout,
			WriteTimeout: writeTimeout,
		}

		c := &tls.Config{
			// Causes servers to use Go's default ciphersuite preferences,
			// which are tuned to avoid attacks. Does nothing on clients.
			PreferServerCipherSuites: true,
			// Only use curves which have assembly implementations
			CurvePreferences: []tls.CurveID{
				tls.CurveP256,
				tls.X25519, // Go 1.8 only
			},
			MinVersion: tls.VersionTLS12,
			CipherSuites: []uint16{
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305, // Go 1.8 only
				tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,   // Go 1.8 only
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,

				// Best disabled, as they don't provide Forward Secrecy,
				// but might be necessary for some clients
				// tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
				// tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
			},
		}
		s.httpsServer = &http.Server{
			Handler:      http.HandlerFunc(s.requestHandler),
			Addr:         httpsAddress,
			TLSConfig:    c,
			ReadTimeout:  readTimeout,
			WriteTimeout: writeTimeout,
		}
	} else {
		s.httpServer = &http.Server{
			Handler:      http.HandlerFunc(s.requestHandler),
			Addr:         httpAddress,
			ReadTimeout:  readTimeout,
			WriteTimeout: writeTimeout,
		}
	}

	return s, nil
}

func (s *WebServer) Start() error {
	if s.httpsServer != nil {
		l, err := net.Listen("tcp", s.httpsServer.Addr)
		if err != nil {
			return err
		}

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			_ = s.httpsServer.ServeTLS(l, s.certFile, s.keyFile)
		}()
	}

	if s.httpServer != nil {
		l, err := net.Listen("tcp", s.httpServer.Addr)
		if err != nil {
			return err
		}

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			_ = s.httpServer.Serve(l)
		}()
	}

	return nil
}

func (s *WebServer) Close() {
	ctx := context.Background()
	if s.httpServer != nil {
		_ = s.httpServer.Shutdown(ctx)
	}
	if s.httpsServer != nil {
		_ = s.httpsServer.Shutdown(ctx)
	}
	s.wg.Wait()
}

func (s *WebServer) SetContentFunc(path string, f WebContentFunc) {
	path = strings.TrimPrefix(path, "/")
	s.lock.Lock()
	defer s.lock.Unlock()

	s.content[path] = WebContent{
		Func: f,
	}
}

func (s *WebServer) SetContent(path string, content []byte, contentType string) {
	s.SetContentWithTime(path, content, contentType, time.Now())
}

func (s *WebServer) SetContentWithTime(
	path string,
	content []byte,
	contentType string,
	modifyTime time.Time,
) {
	path = strings.TrimPrefix(path, "/")
	s.lock.Lock()
	defer s.lock.Unlock()

	s.content[path] = WebContent{
		Content:     content,
		ModifyTime:  modifyTime,
		ContentType: contentType,
	}
}

func (s *WebServer) ContentModifyTime(path string) (time.Time, bool) {
	path = strings.TrimPrefix(path, "/")
	s.lock.Lock()
	defer s.lock.Unlock()

	if c, found := s.content[path]; found {
		return c.ModifyTime, true
	}
	return time.Now(), false
}

func parseBool(s string) bool {
	s = strings.ToLower(s)
	return s == "on" || s == "true" || s == "t" || s == "y" || s == "yes" || s == "1"
}

func (s *WebServer) requestHandler(w http.ResponseWriter, req *http.Request) {
	h := w.Header()
	switch req.URL.Path {
	case "/setjumprun":
		if jumprun == nil {
			http.NotFound(w, req)
			return
		}
		if err := req.ParseForm(); err != nil {
			http.NotFound(w, req)
			return
		}
		if err := jumprun.SetFromURLValues(req.Form); err == nil {
			_ = jumprun.Write()
		}

	case "/setconfig":
		if err := req.ParseForm(); err != nil {
			fmt.Fprintf(os.Stderr, "cannot parse form: %v\n", err)
			http.NotFound(w, req)
			return
		}
		if settings.SetFromURLValues(req.Form) {
			_ = settings.Write()
		}

	default:
		path := strings.TrimPrefix(req.URL.Path, "/")
		s.lock.Lock()
		content, ok := s.content[path]
		s.lock.Unlock()
		if !ok {
			h.Set("Connection", "close")
			http.NotFound(w, req)
		} else if content.Func != nil {
			content.Func(w, req)
		} else {
			h.Set("Content-Type", content.ContentType)
			http.ServeContent(w, req, "", content.ModifyTime,
				bytes.NewReader(content.Content))
		}
	}
}
