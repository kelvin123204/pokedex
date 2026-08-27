package main

import (
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
)

type apiConfig struct {
	fileserverHits atomic.Int32
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func main() {
	mux := http.NewServeMux()
	cfg := apiConfig{
		fileserverHits: atomic.Int32{},
	}

	mux.Handle("GET /app/", cfg.middlewareMetricsInc(http.StripPrefix("/app", http.FileServer(http.Dir(".")))))
	mux.Handle("GET /healthz", cfg.middlewareMetricsInc(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("OK"))
		if err != nil {
			log.Fatal(err)
		}
	})))

	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(fmt.Sprintf("Hits: %d", cfg.fileserverHits.Load())))
		if err != nil {
			log.Fatal(err)
		}
	})

	mux.HandleFunc("POST /reset", func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Store(0)
		w.WriteHeader(http.StatusOK)
	})

	http.ListenAndServe(":8080", mux)
}
