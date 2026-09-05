package main

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/kelvin123204/http-server/internal/auth"
	"github.com/kelvin123204/http-server/internal/database"
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
	Email    string
	Password string
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

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	Id        string `json:"id"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Email     string `json:"email"`
}

type errorResponse struct {
	Error string `json:"error"`
}

var filteringWords = []string{"kerfuffle", "sharbert", "fornax"}

// using interface{} to make it accept any type of data
func writeJson(w http.ResponseWriter, responseStatus int, data interface{}) {
	bytes, e := json.Marshal(data)
	if e != nil {
		writeError(w, http.StatusInternalServerError, errors.New("failed to write json"))
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(responseStatus)
	_, _ = w.Write(bytes)
}

func writeError(w http.ResponseWriter, responseStatus int, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(responseStatus)
	_ = json.NewEncoder(w).Encode(errorResponse{Error: err.Error()})
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal(err)
		return
	}
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
		_, _ = w.Write([]byte("OK"))
	})))

	mux.HandleFunc("GET /admin/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fmt.Sprintf("<html>\n<body>\n<h1>Welcome, Chirpy Admin</h1>\n<p>Chirpy has been visited %d times!</p>\n</body>\n</html>", cfg.fileserverHits.Load())))
	})

	mux.HandleFunc("POST /admin/reset", func(w http.ResponseWriter, r *http.Request) {
		platform := os.Getenv("PLATFORM")
		if platform != "dev" {
			writeError(w, http.StatusInternalServerError, errors.New("only dev can reset the database"))
			return
		}
		err := queries.DeleteUsers(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		cfg.fileserverHits.Store(0)
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("POST /api/users", func(w http.ResponseWriter, r *http.Request) {
		var req createUserRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			err = json.NewEncoder(w).Encode(errorResponse{Error: err.Error()})
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		hp, err := auth.HashPassword(req.Password)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		user, err := queries.CreateUser(r.Context(),
			database.CreateUserParams{
				Email:          req.Email,
				HashedPassword: hp,
			})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		resp := createUserResponse{
			Id:        user.ID.String(),
			CreatedAt: user.CreatedAt.Format(time.RFC3339),
			UpdatedAt: user.UpdatedAt.Format(time.RFC3339),
			Email:     user.Email,
		}
		writeJson(w, http.StatusCreated, resp)
	})

	mux.HandleFunc("POST /api/chirps", func(w http.ResponseWriter, r *http.Request) {
		var req chirpCreateRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if len(req.Body) > 140 {
			writeError(w, http.StatusBadRequest, fmt.Errorf("chirp is too long"))
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
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		writeJson(w, http.StatusCreated, chirpCreateResponse{
			Id:        chirp.ID.String(),
			CreatedAt: chirp.CreatedAt.Format(time.RFC3339),
			UpdatedAt: chirp.UpdatedAt.Format(time.RFC3339),
			Body:      chirp.Body,
			UserId:    chirp.UserID.String(),
		})
	})

	mux.HandleFunc("GET /api/chirps", func(w http.ResponseWriter, r *http.Request) {
		chirps, err := queries.GetChirps(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
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
		writeJson(w, http.StatusOK, resp)
	})

	mux.HandleFunc("GET /api/chirps/{chirpID}", func(w http.ResponseWriter, r *http.Request) {
		chirpID := r.PathValue("chirpID")

		chirp, err := queries.GetChirpById(r.Context(), uuid.MustParse(chirpID))
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, err)
			} else {
				writeError(w, http.StatusInternalServerError, err)
			}
			return
		}
		writeJson(w, http.StatusOK, chirpCreateResponse{
			Id:        chirp.ID.String(),
			CreatedAt: chirp.CreatedAt.Format(time.RFC3339),
			UpdatedAt: chirp.UpdatedAt.Format(time.RFC3339),
			Body:      chirp.Body,
			UserId:    chirp.UserID.String(),
		})
	})

	mux.HandleFunc("POST /api/login", func(w http.ResponseWriter, r *http.Request) {
		var req loginRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		u, err := queries.GetUserByEmail(r.Context(), req.Email)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusUnauthorized, errors.New("incorrect email or password"))
			} else {
				writeError(w, http.StatusInternalServerError, err)
			}
			return
		}
		b, err := auth.CheckPasswordHash(req.Password, u.HashedPassword)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if !b {
			writeError(w, http.StatusUnauthorized, errors.New("incorrect email or password"))
		} else {
			writeJson(w, http.StatusOK, loginResponse{
				Id:        u.ID.String(),
				CreatedAt: u.CreatedAt.Format(time.RFC3339),
				UpdatedAt: u.UpdatedAt.Format(time.RFC3339),
				Email:     u.Email,
			})
		}
	})

	_ = http.ListenAndServe(":8080", mux)
}
