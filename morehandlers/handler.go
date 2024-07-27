package morehandlers // Kullanıcı profili ve ilgili işlemleri yöneten paket

import (
	"database/sql"  // Veritabanı işlemleri için
	"encoding/json" // JSON verilerini işlemek için
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	// Formatlama ve çıktı işlemleri için
	"form-project/datahandlers" // Veritabanı bağlantısı ve oturum yönetimi için
	"form-project/utils"        // Hata yönetimi gibi yardımcı fonksiyonlar için
	// HTML şablonlarını işlemek için
	// HTTP isteklerini ve yanıtlarını yönetmek için
	// String (metin) işlemleri için
	// Zaman ve tarih işlemleri için
)

// Post yapısı, bir gönderinin verilerini temsil eder.
type Post struct {
	ID                  int       // Gönderi ID'si
	UserID              int       // Gönderi sahibi kullanıcı ID'si
	Title               string    // Gönderi başlığı
	Content             string    // Gönderi içeriği
	Categories          []string  // Gönderi kategorileri (JSON olarak saklanır)
	CategoriesFormatted string    // Gönderi kategorileri (virgülle ayrılmış, görüntüleme amaçlı)
	CreatedAt           time.Time // Gönderi oluşturulma tarihi
	CreatedAtFormatted  string    // Gönderi oluşturulma tarihi (formatlı)
	LikeCount           int       // Gönderi beğeni sayısı
	DislikeCount        int       // Gönderi beğenmeme sayısı
	Username            string    // Gönderi sahibi kullanıcı adı
	CommentCount        int       // Gönderiye yapılan yorum sayısı
}

// User yapısı, bir kullanıcıyı temsil eder.
type User struct {
	ID                 int            `validate:"-"`
	Email              string         `validate:"required,email"`
	Username           sql.NullString // Google kayıtta bazen boş olabilir
	Password           sql.NullString // Google kayıtta şifre alanı gereksiz olabilir
	ProfilePicturePath sql.NullString // Profil fotoğrafının yolu
}

// kullanıcının profil sayfasını oluşturur ve görüntüler.
func MyProfileHandler(w http.ResponseWriter, r *http.Request) {
	session, err := datahandlers.GetSession(r)
	if err != nil || session == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	user, err := getUserByID(session.UserID)
	if err != nil {
		utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
		return
	}

	ownPosts, err := getOwnPosts(session.UserID)
	if err != nil {
		utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
		return
	}

	likedPosts, err := getLikedPosts(session.UserID)
	if err != nil {
		utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
		return
	}

	tmpl, err := template.ParseFiles("templates/myprofil.html")
	if err != nil {
		utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
		return
	}

	data := struct {
		User       *User
		OwnPosts   []Post
		LikedPosts []Post
	}{
		User:       user,
		OwnPosts:   ownPosts,
		LikedPosts: likedPosts,
	}

	err = tmpl.Execute(w, data)
	if err != nil {
		utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// Belirtilen kullanıcı ID'sine ait gönderileri veritabanından çeker.
func getOwnPosts(userID int) ([]Post, error) {
	query := `SELECT posts.id, posts.user_id, posts.title, posts.content, posts.categories, posts.created_at, users.username,
                     COALESCE(SUM(CASE WHEN votes.vote_type = 1 THEN 1 ELSE 0 END), 0) AS like_count,
                     COALESCE(SUM(CASE WHEN votes.vote_type = -1 THEN 1 ELSE 0 END), 0) AS dislike_count,
                     (SELECT COUNT(*) FROM comments WHERE comments.post_id = posts.id AND comments.deleted = 0) AS comment_count
              FROM posts
              JOIN users ON posts.user_id = users.id
              LEFT JOIN votes ON votes.post_id = posts.id
              WHERE posts.user_id = ? AND posts.deleted = 0
              GROUP BY posts.id
              ORDER BY posts.created_at DESC`

	rows, err := datahandlers.DB.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []Post
	for rows.Next() {
		var post Post
		var categoriesJSON string
		if err := rows.Scan(&post.ID, &post.UserID, &post.Title, &post.Content, &categoriesJSON, &post.CreatedAt, &post.Username, &post.LikeCount, &post.DislikeCount, &post.CommentCount); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(categoriesJSON), &post.Categories); err != nil {
			return nil, err
		}

		post.CategoriesFormatted = strings.Join(post.Categories, ", ")
		post.CreatedAtFormatted = post.CreatedAt.Format("2006-01-02 15:04")
		posts = append(posts, post)
	}
	return posts, nil
}

// Belirtilen kullanıcı ID'sinin beğendiği gönderileri veritabanından çeker.
func getLikedPosts(userID int) ([]Post, error) {
	query := `
		SELECT posts.id, posts.user_id, posts.title, posts.content, posts.categories, posts.created_at, users.username,
		       COALESCE(SUM(CASE WHEN votes.vote_type = 1 THEN 1 ELSE 0 END), 0) AS like_count,
		       COALESCE(SUM(CASE WHEN votes.vote_type = -1 THEN 1 ELSE 0 END), 0) AS dislike_count,
		       (SELECT COUNT(*) FROM comments WHERE comments.post_id = posts.id AND comments.deleted = 0) AS comment_count
		FROM posts
		JOIN users ON posts.user_id = users.id
		LEFT JOIN votes ON votes.post_id = posts.id
		WHERE posts.id IN (SELECT post_id FROM votes WHERE user_id = ? AND vote_type = 1)
		AND posts.deleted = 0
		GROUP BY posts.id, posts.user_id, posts.title, posts.content, posts.categories, posts.created_at, users.username
		ORDER BY posts.created_at DESC`

	rows, err := datahandlers.DB.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []Post
	for rows.Next() {
		var post Post
		var categoriesJSON string
		if err := rows.Scan(&post.ID, &post.UserID, &post.Title, &post.Content, &categoriesJSON, &post.CreatedAt, &post.Username, &post.LikeCount, &post.DislikeCount, &post.CommentCount); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(categoriesJSON), &post.Categories); err != nil {
			return nil, err
		}

		post.CategoriesFormatted = strings.Join(post.Categories, ", ")
		post.CreatedAtFormatted = post.CreatedAt.Format("2006-01-02 15:04")
		posts = append(posts, post)
	}
	return posts, nil
}

// Belirtilen kullanıcı ID'sine sahip kullanıcıyı veritabanından çeker.
func getUserByID(userID int) (*User, error) {
	var user User
	query := "SELECT id, email, username, password, profile_picture_path FROM users WHERE id = ?"
	err := datahandlers.DB.QueryRow(query, userID).Scan(&user.ID, &user.Email, &user.Username, &user.Password, &user.ProfilePicturePath)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user with ID %d not found", userID)
		}
		return nil, err
	}
	return &user, nil
}

const maxUploadSize = 20 * 1024 * 1024 // 20 MB
