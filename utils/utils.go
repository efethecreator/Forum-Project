package utils

import (
	"encoding/json"
	"log"
	"net/http"
)

func HandleErr(w http.ResponseWriter, err error, message string, statusCode int) {
    log.Println(err)
    w.WriteHeader(statusCode)
    w.Header().Set("Content-Type", "application/json")

    // Hata mesajını JSON olarak gönder
    response := map[string]string{"error": message}
    json.NewEncoder(w).Encode(response)
}
