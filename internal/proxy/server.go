package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"caching-proxy/internal/cache"
)

// Run starts an HTTP server on the given port and proxies to the provided origin base URL.
// It blocks until the context is cancelled, at which point it gracefully shuts down.
func Run(ctx context.Context, port int, originBase string, c cache.Cache, debug bool) error {
	target, err := url.Parse(originBase)
	if err != nil {
		return fmt.Errorf("invalid origin: %w", err)
	}

	baseProxy := newReverseProxy(target)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lw := &loggingWriter{ResponseWriter: w, status: http.StatusOK}

		cacheEligible := r.Method == http.MethodGet && r.Header.Get("Authorization") == "" && !reqNoStore(r)

		if c != nil && cacheEligible {
			key, err := cache.BuildCacheKey(originBase, r)
			if err == nil {
				if ent, ok, _ := c.Get(key); ok && ent != nil {
					// cache HIT
					copyHeaders(lw.Header(), ent.Header)
					lw.Header().Set("X-Cache", "HIT")
					filterHopByHop(lw.Header())
					lw.WriteHeader(ent.Status)
					_, _ = lw.Write(ent.Body)
					dur := time.Since(start)
					if debug {
						log.Printf("%s %s -> %d %dB %s (HIT)", r.Method, requestLine(r), lw.status, lw.bytes, dur)
					} else {
						log.Printf("%s %s -> %d %s", r.Method, requestLine(r), lw.status, dur)
					}
					return
				}

				// cache MISS: use a per-request proxy to capture and store
				rp := newReverseProxy(target)
				rp.ModifyResponse = func(res *http.Response) error {
					filterHopByHop(res.Header)
					res.Header.Set("X-Cache", "MISS")
					if resNoStore(res) {
						return nil
					}
					// buffer body
					b, err := io.ReadAll(res.Body)
					if err != nil {
						return err
					}
					_ = res.Body.Close()
					res.Body = io.NopCloser(bytes.NewReader(b))
					res.ContentLength = int64(len(b))
					res.Header.Set("Content-Length", fmt.Sprintf("%d", len(b)))
					// store
					entry := &cache.Entry{Status: res.StatusCode, Header: cache.CloneHeaders(res.Header), Body: b}
					_ = c.Set(key, entry)
					return nil
				}
				// Serve via this rp
				rp.ServeHTTP(lw, r)
				dur := time.Since(start)
				if debug {
					log.Printf("%s %s -> %d %dB %s (MISS)", r.Method, requestLine(r), lw.status, lw.bytes, dur)
				} else {
					log.Printf("%s %s -> %d %s", r.Method, requestLine(r), lw.status, dur)
				}
				return
			}
		}

		// Not cache-eligible or cache disabled: use base proxy
		baseProxy.ServeHTTP(lw, r)
		dur := time.Since(start)
		if debug {
			log.Printf("%s %s -> %d %dB %s", r.Method, requestLine(r), lw.status, lw.bytes, dur)
		} else {
			log.Printf("%s %s -> %d %s", r.Method, requestLine(r), lw.status, dur)
		}
	})

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Run server
	errCh := make(chan error, 1)
	go func() {
		log.Printf("listening on %s, forwarding to %s", srv.Addr, target.String())
		err := srv.ListenAndServe()
		if !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		return err
	}
}

func newReverseProxy(target *url.URL) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(target)

	// Director: ensure scheme/host are set to origin, preserve path and query.
	origDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		origDirector(req)
		// Remove hop-by-hop request headers (basic set; expanded later)
		filterHopByHop(req.Header)
		// Set Host header to origin host when proxying
		req.Host = target.Host
	}

	proxy.ModifyResponse = func(res *http.Response) error {
		// Remove hop-by-hop response headers (basic set; expanded later)
		filterHopByHop(res.Header)
		return nil
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("proxy error: %v", err)
		http.Error(w, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
	}

	return proxy
}

var hopHeaders = []string{
	"Connection",
	"Proxy-Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"TE",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

func filterHopByHop(h http.Header) {
	// Remove headers listed in Connection as well as common hop-by-hop headers
	if c := h.Get("Connection"); c != "" {
		for _, f := range strings.Split(c, ",") {
			name := strings.TrimSpace(f)
			if name != "" {
				h.Del(name)
			}
		}
	}
	for _, hh := range hopHeaders {
		h.Del(hh)
	}
}

type loggingWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *loggingWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *loggingWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

func requestLine(r *http.Request) string {
	q := r.URL.RawQuery
	if q == "" {
		return r.URL.Path
	}
	return r.URL.Path + "?" + q
}

func copyHeaders(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func reqNoStore(r *http.Request) bool {
	cc := r.Header.Get("Cache-Control")
	return containsToken(cc, "no-store")
}

func resNoStore(res *http.Response) bool {
	cc := res.Header.Get("Cache-Control")
	return containsToken(cc, "no-store")
}

func containsToken(v, token string) bool {
	for _, part := range strings.Split(v, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}
