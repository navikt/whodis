package routes

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/navikt/whodis/internal/github"
)

func GetRoot(w http.ResponseWriter, _ *http.Request) {
	_, err := w.Write([]byte("whodis?"))
	if err != nil {
		fmt.Printf("Error writing response: %v", err)
	}
}

func GetTest(w http.ResponseWriter, r *http.Request) {
	ghUser := r.PathValue("githubUser")
	email := github.EmailFor(ghUser)
	if email != "" {
		err := json.NewEncoder(w).Encode(email)
		if err != nil {
			fmt.Printf("Error encoding JSON: %v\n", err)
		}
	} else {
		http.Error(w, http.StatusText(403), 403)
	}
}

func GetJkTest(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func GetLiveness(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func GetReadyness(w http.ResponseWriter, _ *http.Request) {
	if github.UsersAreLoaded() {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusPreconditionFailed)
	}
}
