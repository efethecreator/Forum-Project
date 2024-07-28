package posthandlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"form-project/datahandlers"
	"form-project/homehandlers"
	"form-project/utils"

	"github.com/google/uuid"
)

type Post struct {
	ID                  int
	UserID              int
	Title               string
	Content             string
	Categories          []string // JSON olarak kaydedilecek ve geri okunacak
	CategoriesFormatted string   // Virgülle ayrılmış kategoriler
	CreatedAt           time.Time
	CreatedAtFormatted  string
	LikeCount           int
	DislikeCount        int
	Username            string
	CommentCount        int
	ImagePath           string
}

type Comment struct {
	ID                 int
	PostID             int
	UserID             int
	Content            string
	CreatedAt          time.Time
	CreatedAtFormatted string
	LikeCount          int
	DislikeCount       int
	Username           string // Kullanıcı adı
	ImagePath          string // Add this line to include ImagePath
}

const maxUploadSize = 20 * 1024 * 1024 // 20 MB

func CreatePostHandler(w http.ResponseWriter, r *http.Request) {
	session, err := datahandlers.GetSession(r)
	if err != nil || session == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if r.Method == http.MethodPost {
		// Form verilerini al
		title := r.FormValue("title")
		content := r.FormValue("content")
		categoriesJSON := r.FormValue("categories")

		// Kategorileri JSON'dan ayrıştır
		var categories []string
		err := json.Unmarshal([]byte(categoriesJSON), &categories)
		if err != nil {
			utils.HandleErr(w, err, "Invalid categories format", http.StatusBadRequest)
			return
		}

		// Fotoğrafı işle
		var imagePath string
		newFilename := ""

		file, handler, err := r.FormFile("image")
		if err != nil && err != http.ErrMissingFile {
			utils.HandleErr(w, err, "Error getting image", http.StatusBadRequest)
			return
		}

		if file != nil { // Dosya varsa işle
			defer file.Close()

			// Dosya boyutunu kontrol et
			if handler.Size > maxUploadSize {
				utils.HandleErr(w, fmt.Errorf("file size exceeds limit"), "File size exceeds limit (20MB)", http.StatusBadRequest)
				return
			}

			// Dosya uzantısını kontrol et
			ext := filepath.Ext(handler.Filename)
			allowedExtensions := map[string]bool{
				".jpg":  true,
				".jpeg": true,
				".png":  true,
				".gif":  true,
			}
			if !allowedExtensions[ext] {
				utils.HandleErr(w, fmt.Errorf("unsupported image format: %s", ext), "Unsupported image format", http.StatusBadRequest)
				return
			}

			// Benzersiz dosya adı oluştur ve newFilename'e ata
			imageUUID := uuid.New().String()
			newFilename = imageUUID + ext

			// uploads dizininin var olup olmadığını kontrol et, yoksa oluştur
			uploadsDir := "./uploads"
			if _, err := os.Stat(uploadsDir); os.IsNotExist(err) {
				os.Mkdir(uploadsDir, 0o755) // Klasörü oluştur
			}

			// Kaydedilecek dosyanın tam yolunu oluştur
			imagePath = filepath.Join(uploadsDir, newFilename)

			// Fotoğrafı kaydet
			if err := saveImage(file, imagePath); err != nil {
				utils.HandleErr(w, err, "Error saving image", http.StatusInternalServerError)
				return
			}
		}

		categoriesData, err := json.Marshal(categories)
		if err != nil {
			utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Veritabanına kaydet (imagePath ile birlikte)
		_, err = datahandlers.DB.Exec("INSERT INTO posts (user_id, title, content, categories, created_at, image_path) VALUES (?, ?, ?, ?, ?, ?)",
			session.UserID, title, content, string(categoriesData), time.Now(), newFilename)
		if err != nil {
			utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	tmpl, err := template.ParseFiles("templates/createPost.html")
	if err != nil {
		utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
		return
	}
	err = tmpl.Execute(w, nil)
	if err != nil {
		utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
	}
}

// Fotoğrafı kaydet
func saveImage(file multipart.File, imagePath string) error {
	// Dosyayı aç
	dst, err := os.Create(imagePath)
	if err != nil {
		return fmt.Errorf("dosya oluşturulamadı: %w", err)
	}
	defer dst.Close()

	// Gelen veriyi dosyaya kopyala
	_, err = io.Copy(dst, file)
	if err != nil {
		return fmt.Errorf("dosya kopyalanamadı: %w", err)
	}

	return nil
}

// Bir gönderiye yeni bir yorum eklemek için kullanılan HTTP işleyicisidir.
func CreateCommentHandler(w http.ResponseWriter, r *http.Request) {
	session, err := datahandlers.GetSession(r)
	if err != nil || session == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if r.Method == http.MethodPost {
		err := r.ParseMultipartForm(32 << 20) // 32 MB maksimum upload boyutu
		if err != nil {
			utils.HandleErr(w, err, "Error parsing multipart form", http.StatusBadRequest)
			return
		}

		postIDStr := r.FormValue("post_id")
		content := r.FormValue("content")

		if content == "" {
			utils.HandleErr(w, nil, "Content is required", http.StatusBadRequest)
			return
		}

		postID, err := strconv.Atoi(postIDStr)
		if err != nil {
			utils.HandleErr(w, err, "Invalid post ID", http.StatusBadRequest)
			return
		}

		// Yorum fotoğrafını işle
		var commentImagePath string
		newFilename := ""

		file, handler, err := r.FormFile("commentImage")
		if err == nil { // Dosya varsa işle
			defer file.Close()

			// Dosya boyutunu kontrol et
			if handler.Size > maxUploadSize {
				utils.HandleErr(w, fmt.Errorf("file size exceeds limit"), "File size exceeds limit (20MB)", http.StatusBadRequest)
				return
			}

			// Dosya uzantısını kontrol et
			ext := filepath.Ext(handler.Filename)
			allowedExtensions := map[string]bool{
				".jpg":  true,
				".jpeg": true,
				".png":  true,
				".gif":  true,
			}
			if !allowedExtensions[ext] {
				utils.HandleErr(w, fmt.Errorf("unsupported image format: %s", ext), "Unsupported image format", http.StatusBadRequest)
				return
			}

			// Benzersiz dosya adı oluştur ve newFilename'e ata
			imageUUID := uuid.New().String()
			newFilename = imageUUID + ext

			// uploads dizininin var olup olmadığını kontrol et, yoksa oluştur
			uploadsDir := "./uploads"
			if _, err := os.Stat(uploadsDir); os.IsNotExist(err) {
				os.Mkdir(uploadsDir, 0o755) // Klasörü oluştur
			}

			// Kaydedilecek dosyanın tam yolunu oluştur
			commentImagePath = filepath.Join(uploadsDir, newFilename)

			// Fotoğrafı kaydet
			if err := saveImage(file, commentImagePath); err != nil {
				utils.HandleErr(w, err, "Error saving image", http.StatusInternalServerError)
				return
			}
		} else if err != http.ErrMissingFile { // Dosya yoksa hata ver
			utils.HandleErr(w, err, "Error getting image", http.StatusBadRequest)
			return
		}

		// Veritabanına kaydet (commentImagePath ile birlikte)
		_, err = datahandlers.DB.Exec("INSERT INTO comments (post_id, user_id, content, created_at, image_path) VALUES (?, ?, ?, ?, ?)",
			postID, session.UserID, content, time.Now(), newFilename)
		if err != nil {
			utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, fmt.Sprintf("/viewPost?id=%d", postID), http.StatusSeeOther)
		return
	}
}

// Belirli bir yorumu silmek için kullanılan HTTP işleyicisidir.
func DeletePostHandler(w http.ResponseWriter, r *http.Request) {
	session, err := datahandlers.GetSession(r)
	if err != nil || session == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Admin olup olmadığını kontrol et
	isAdmin, err := homehandlers.CheckIfAdmin(int64(session.UserID)) // int dönüşümü
	if err != nil || !isAdmin {
		http.Redirect(w, r, "/", http.StatusForbidden)
		return
	}

	postID := strings.TrimPrefix(r.URL.Path, "/posts/delete/")
	if postID == "" {
		http.Error(w, "Post ID is required", http.StatusBadRequest)
		return
	}

	_, err = datahandlers.DB.Exec("DELETE FROM posts WHERE id = ?", postID)
	if err != nil {
		utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// Belirli bir yorumu silmek için kullanılan HTTP işleyicisidir.
func DeleteCommentHandler(w http.ResponseWriter, r *http.Request) {
	session, err := datahandlers.GetSession(r)
	if err != nil || session == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	commentID := r.FormValue("comment_id")
	if commentID == "" {
		http.Error(w, "Comment ID is required", http.StatusBadRequest)
		return
	}

	var userID, postID int
	err = datahandlers.DB.QueryRow("SELECT user_id, post_id FROM comments WHERE id = ?", commentID).Scan(&userID, &postID)
	if err != nil {
		utils.HandleErr(w, err, "Comment not found", http.StatusNotFound)
		return
	}

	var postOwnerID int
	err = datahandlers.DB.QueryRow("SELECT user_id FROM posts WHERE id = ?", postID).Scan(&postOwnerID)
	if err != nil {
		utils.HandleErr(w, err, "Post not found", http.StatusNotFound)
		return
	}

	if userID != session.UserID && postOwnerID != session.UserID {
		http.Error(w, "You can only delete your own comments or comments on your posts", http.StatusForbidden)
		return
	}

	_, err = datahandlers.DB.Exec("UPDATE comments SET deleted = 1 WHERE id = ?", commentID)
	if err != nil {
		utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/viewPost?id=%d", postID), http.StatusSeeOther)
}

func ReportPostHandler(w http.ResponseWriter, r *http.Request) {
	session, err := datahandlers.GetSession(r)
	if err != nil || session == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	postID := r.FormValue("post_id")
	if postID == "" {
		http.Error(w, "Post ID is required", http.StatusBadRequest)
		return
	}

	_, err = datahandlers.DB.Exec("INSERT INTO reports (post_id, user_id) VALUES (?, ?)", postID, session.UserID)
	if err != nil {
		utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// Gönderilere veya yorumlara oy vermek (beğenmek/beğenmemek) için kullanılır.
func VoteHandler(w http.ResponseWriter, r *http.Request) {
	session, err := datahandlers.GetSession(r)
	if err != nil || session == nil {
		w.WriteHeader(http.StatusUnauthorized)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"redirect": "/login"})
		return
	}

	postID := r.FormValue("post_id")
	commentID := r.FormValue("comment_id")
	voteTypeStr := r.FormValue("vote_type")

	voteType, err := strconv.Atoi(voteTypeStr)
	if err != nil || (voteType != 1 && voteType != -1) {
		utils.HandleErr(w, err, "Invalid vote type", http.StatusBadRequest)
		return
	}

	var existingVoteType sql.NullInt64
	var query string

	if postID != "" {
		query = "SELECT vote_type FROM votes WHERE user_id = ? AND post_id = ?"
		err = datahandlers.DB.QueryRow(query, session.UserID, postID).Scan(&existingVoteType)
	} else if commentID != "" {
		query = "SELECT vote_type FROM votes WHERE user_id = ? AND comment_id = ?"
		err = datahandlers.DB.QueryRow(query, session.UserID, commentID).Scan(&existingVoteType)
	}

	if err != nil && err != sql.ErrNoRows {
		utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
		return
	}

	if existingVoteType.Valid {
		if existingVoteType.Int64 == int64(voteType) {
			if postID != "" {
				query = "DELETE FROM votes WHERE user_id = ? AND post_id = ?"
				_, err = datahandlers.DB.Exec(query, session.UserID, postID)
			} else if commentID != "" {
				query = "DELETE FROM votes WHERE user_id = ? AND comment_id = ?"
				_, err = datahandlers.DB.Exec(query, session.UserID, commentID)
			}
		} else {
			if postID != "" {
				query = "UPDATE votes SET vote_type = ? WHERE user_id = ? AND post_id = ?"
				_, err = datahandlers.DB.Exec(query, voteType, session.UserID, postID)
			} else if commentID != "" {
				query = "UPDATE votes SET vote_type = ? WHERE user_id = ? AND comment_id = ?"
				_, err = datahandlers.DB.Exec(query, voteType, session.UserID, commentID)
			}
		}
	} else {
		if postID != "" {
			query = "INSERT INTO votes (user_id, post_id, vote_type) VALUES (?, ?, ?)"
			_, err = datahandlers.DB.Exec(query, session.UserID, postID, voteType)
		} else if commentID != "" {
			query = "INSERT INTO votes (user_id, comment_id, vote_type) VALUES (?, ?, ?)"
			_, err = datahandlers.DB.Exec(query, session.UserID, commentID, voteType)
		}
	}

	if err != nil {
		utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Oy sayısını yeniden hesapla ve JSON olarak dön
	var likeCount, dislikeCount int
	if postID != "" {
		err = datahandlers.DB.QueryRow(`SELECT 
			COALESCE(SUM(CASE WHEN vote_type = 1 THEN 1 ELSE 0 END), 0) AS like_count,
			COALESCE(SUM(CASE WHEN vote_type = -1 THEN 1 ELSE 0 END), 0) AS dislike_count
			FROM votes WHERE post_id = ?`, postID).Scan(&likeCount, &dislikeCount)
	} else if commentID != "" {
		err = datahandlers.DB.QueryRow(`SELECT 
			COALESCE(SUM(CASE WHEN vote_type = 1 THEN 1 ELSE 0 END), 0) AS like_count,
			COALESCE(SUM(CASE WHEN vote_type = -1 THEN 1 ELSE 0 END), 0) AS dislike_count
			FROM votes WHERE comment_id = ?`, commentID).Scan(&likeCount, &dislikeCount)
	}

	if err != nil {
		utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
		return
	}

	response := map[string]int{"like_count": likeCount, "dislike_count": dislikeCount}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// Belirli bir gönderiyi ve altındaki yorumları görüntülemek için kullanılan HTTP işleyicisidir.
func ViewPostHandler(w http.ResponseWriter, r *http.Request) {
	session, _ := datahandlers.GetSession(r)

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	postIDStr := r.URL.Query().Get("id")
	if postIDStr == "" {
		http.Error(w, "Post ID required", http.StatusBadRequest)
		return
	}

	postID, err := strconv.Atoi(postIDStr)
	if err != nil {
		http.Error(w, "Invalid post ID", http.StatusBadRequest)
		return
	}

	var post Post
	var categoriesJSON string
	err = datahandlers.DB.QueryRow(`SELECT posts.id, posts.user_id, posts.title, posts.content, posts.categories, posts.created_at, users.username, posts.image_path, 
        COALESCE(SUM(CASE WHEN votes.vote_type = 1 THEN 1 ELSE 0 END), 0) AS like_count,
        COALESCE(SUM(CASE WHEN votes.vote_type = -1 THEN 1 ELSE 0 END), 0) AS dislike_count
        FROM posts
        JOIN users ON posts.user_id = users.id
        LEFT JOIN votes ON votes.post_id = posts.id
        WHERE posts.id = ? AND posts.deleted = 0
        GROUP BY posts.id`, postID).Scan(&post.ID, &post.UserID, &post.Title, &post.Content, &categoriesJSON, &post.CreatedAt, &post.Username, &post.ImagePath, &post.LikeCount, &post.DislikeCount)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Post not found", http.StatusNotFound)
		} else {
			log.Println("Error querying post:", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}

	var categories []string
	err = json.Unmarshal([]byte(categoriesJSON), &categories)
	if err != nil {
		utils.HandleErr(w, err, "Error parsing categories", http.StatusInternalServerError)
		return
	}

	post.CreatedAtFormatted = post.CreatedAt.Format("2006-01-02 15:04")
	post.Categories = categories

	rows, err := datahandlers.DB.Query(`
        SELECT c.id, c.post_id, c.user_id, c.content, c.created_at, u.username, c.image_path, 
               COALESCE(SUM(CASE WHEN v.vote_type = 1 THEN 1 ELSE 0 END), 0) AS like_count,
               COALESCE(SUM(CASE WHEN v.vote_type = -1 THEN 1 ELSE 0 END), 0) AS dislike_count
        FROM comments c
        JOIN users u ON c.user_id = u.id
        LEFT JOIN votes v ON v.comment_id = c.id
        WHERE c.post_id = ? AND c.deleted = 0
        GROUP BY c.id, c.post_id, c.user_id, c.content, c.created_at, u.username, c.image_path 
        ORDER BY c.created_at DESC`, postID)
	if err != nil {
		log.Println("Error querying comments:", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var comments []Comment
	for rows.Next() {
		var comment Comment
		err := rows.Scan(&comment.ID, &comment.PostID, &comment.UserID, &comment.Content, &comment.CreatedAt, &comment.Username, &comment.ImagePath, &comment.LikeCount, &comment.DislikeCount)
		if err != nil {
			log.Println("Error scanning comment:", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		comment.CreatedAtFormatted = comment.CreatedAt.Format("2006-01-02 15:04")
		comments = append(comments, comment)
	}
	if err := rows.Err(); err != nil {
		log.Println("Rows error:", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	data := struct {
		Post     Post
		Comments []Comment
		LoggedIn bool
	}{
		Post:     post,
		Comments: comments,
		LoggedIn: session != nil,
	}

	tmpl, err := template.ParseFiles("templates/viewPost.html")
	if err != nil {
		log.Println("Error parsing template:", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	err = tmpl.Execute(w, data)
	if err != nil {
		log.Println("Error executing template:", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
