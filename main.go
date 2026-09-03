package main

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/kelvin123204/pokedex/internal/database"
	_ "github.com/lib/pq"
)

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
)

type apiConfig struct {
	fileserverHits atomic.Int32
}

type createUserRequest struct {
	Email string
}

type createUserResponse struct {
	Id        string `json:"id"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Email     string `json:"email"`
}

type chirpCreateRequest struct {
	Body   string `json:"body"`
	UserId string `json:"user_id"`
}

type chirpCreateResponse struct {
	Id        string `json:"id"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Body      string `json:"body"`
	UserId    string `json:"user_id"`
}

type chirpResponse struct {
	CleanedBody string `json:"cleaned_body"`
}

type errorResponse struct {
	Error string `json:"error"`
}

var filteringWords = []string{"kerfuffle", "sharbert", "fornax"}

func writeError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	e := json.NewEncoder(w).Encode(errorResponse{Error: err.Error()})
	if e != nil {
		log.Fatal(e)
	}
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func main() {
	godotenv.Load()
	dbURL := os.Getenv("DB_URL")
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatal(err)
		return
	}
	queries := database.New(db)

	mux := http.NewServeMux()
	cfg := apiConfig{
		fileserverHits: atomic.Int32{},
	}

	mux.Handle("GET /app/", cfg.middlewareMetricsInc(http.StripPrefix("/app", http.FileServer(http.Dir(".")))))
	mux.Handle("GET /api/healthz", cfg.middlewareMetricsInc(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("OK"))
		if err != nil {
			log.Fatal(err)
		}
	})))

	mux.HandleFunc("GET /admin/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(fmt.Sprintf("<html>\n<body>\n<h1>Welcome, Chirpy Admin</h1>\n<p>Chirpy has been visited %d times!</p>\n</body>\n</html>", cfg.fileserverHits.Load())))
		if err != nil {
			log.Fatal(err)
		}
	})

	mux.HandleFunc("POST /admin/reset", func(w http.ResponseWriter, r *http.Request) {
		platform := os.Getenv("PLATFORM")
		if platform != "dev" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		err := queries.DeleteUsers(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		cfg.fileserverHits.Store(0)
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("POST /api/users", func(w http.ResponseWriter, r *http.Request) {
		var req createUserRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			err = json.NewEncoder(w).Encode(errorResponse{Error: err.Error()})
			if err != nil {
				log.Fatal(err)
			}
			return
		}
		user, err := queries.CreateUser(r.Context(), req.Email)
		if err != nil {
			writeError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		resp := createUserResponse{
			Id:        user.ID.String(),
			CreatedAt: user.CreatedAt.Format(time.RFC3339),
			UpdatedAt: user.UpdatedAt.Format(time.RFC3339),
			Email:     user.Email,
		}
		err = json.NewEncoder(w).Encode(resp)
		if err != nil {
			writeError(w, err)
			return
		}
	})

	mux.HandleFunc("POST /api/chirps", func(w http.ResponseWriter, r *http.Request) {
		var req chirpCreateRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			err = json.NewEncoder(w).Encode(errorResponse{Error: err.Error()})
			if err != nil {
				log.Fatal(err)
			}
			return
		}

		if len(req.Body) > 140 {
			writeError(w, fmt.Errorf("chirp is too long"))
			return
		}
		cleanedBody := req.Body
		regex := fmt.Sprintf(`(%s)`, strings.Join(filteringWords, "|"))
		cleanedBody = regexp.MustCompile("(?i)"+regex).ReplaceAllStringFunc(
			cleanedBody,
			func(s string) string {
				return "****"
			},
		)

		chirp, err := queries.CreateChirps(r.Context(), database.CreateChirpsParams{
			Body:   cleanedBody,
			UserID: uuid.MustParse(req.UserId),
		})

		if err != nil {
			writeError(w, err)
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		resp := chirpCreateResponse{
			Id:        chirp.ID.String(),
			CreatedAt: chirp.CreatedAt.Format(time.RFC3339),
			UpdatedAt: chirp.UpdatedAt.Format(time.RFC3339),
			Body:      chirp.Body,
			UserId:    chirp.UserID.String(),
		}
		err = json.NewEncoder(w).Encode(resp)
		if err != nil {
			writeError(w, err)
			return
		}
	})

	mux.HandleFunc("GET /api/chirps", func(w http.ResponseWriter, r *http.Request) {
		chirps, err := queries.GetChirps(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		var resp []chirpCreateResponse
		for _, chirp := range chirps {
			resp = append(resp, chirpCreateResponse{
				Id:        chirp.ID.String(),
				CreatedAt: chirp.CreatedAt.Format(time.RFC3339),
				UpdatedAt: chirp.UpdatedAt.Format(time.RFC3339),
				Body:      chirp.Body,
				UserId:    chirp.UserID.String(),
			})
		}
		err = json.NewEncoder(w).Encode(resp)
		if err != nil {
			writeError(w, err)
			return
		}
	})

	mux.HandleFunc("GET /api/chirps/{chirpID}", func(w http.ResponseWriter, r *http.Request) {
		chirpID := r.PathValue("chirpID")

		chirp, err := queries.GetChirpById(r.Context(), uuid.MustParse(chirpID))
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			writeError(w, err)
			return

		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		resp := chirpCreateResponse{
			Id:        chirp.ID.String(),
			CreatedAt: chirp.CreatedAt.Format(time.RFC3339),
			UpdatedAt: chirp.UpdatedAt.Format(time.RFC3339),
			Body:      chirp.Body,
			UserId:    chirp.UserID.String(),
		}
		err = json.NewEncoder(w).Encode(resp)
		if err != nil {
			writeError(w, err)
			return
		}
	})

	http.ListenAndServe(":8080", mux)
}
