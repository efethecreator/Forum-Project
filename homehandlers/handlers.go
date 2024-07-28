package homehandlers

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"

	"form-project/datahandlers"
	"form-project/utils"

	"github.com/go-playground/validator"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/facebook"
	"golang.org/x/oauth2/github"
	"golang.org/x/oauth2/google"
)

type User struct {
	ID       int            `validate:"-"`
	Email    string         `validate:"required,email"`
	Username sql.NullString // Google kayıtta bazen boş olabilir
	Role     string
	Password sql.NullString // Google kayıtta şifre alanı gereksiz olabilir
}

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
}

type RegisterTemplateData struct {
	ErrorMessages map[string]string
	Email         string
	Username      string
}

type AdminTemplateData struct {
	Users    []User
	Posts    []Post
	LoggedIn bool
	IsAdmin  bool
}

var (
	validate    = validator.New()
	config      Config
	registering = false // Kaydetme veya giriş yapma işlemini ayırt etmek için
)

type Config struct {
	GoogleClientID       string `json:"google_client_id"`
	GoogleClientSecret   string `json:"google_client_secret"`
	GitHubClientID       string `json:"github_client_id"`
	GitHubClientSecret   string `json:"github_client_secret"`
	FacebookClientID     string `json:"facebook_client_id"`
	FacebookClientSecret string `json:"facebook_client_secret"`
}

func loadConfig() {
	file, err := os.Open("config.json")
	if err != nil {
		log.Fatalf("Failed to open config file: %s", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	err = decoder.Decode(&config)
	if err != nil {
		log.Fatalf("Failed to decode config file: %s", err)
	}
}

// OAuth 2.0 yapılandırmalarını tutmak için genel değişkenler
var (
	googleOauthConfig      *oauth2.Config // googleOauthConfig değişkeni, Google OAuth 2.0 yapılandırmasını tutar.
	githubOauthConfig      *oauth2.Config
	facebookOauthConfig    *oauth2.Config
	oauthStateStringGoogle string // Google OAuth durumu için
)

// Paket yüklenirken otomatik olarak çalışır.
func init() {
	loadConfig()
	// Google OAuth 2.0 yapılandırması oluşturulur.
	googleOauthConfig = &oauth2.Config{
		RedirectURL:  "http://localhost:8065/google/callback",
		ClientID:     config.GoogleClientID,
		ClientSecret: config.GoogleClientSecret,
		Scopes:       []string{"https://www.googleapis.com/auth/userinfo.email", "https://www.googleapis.com/auth/userinfo.profile"},
		Endpoint:     google.Endpoint,
	}

	githubOauthConfig = &oauth2.Config{
		RedirectURL:  "http://localhost:8065/github/callback",
		ClientID:     config.GitHubClientID,
		ClientSecret: config.GitHubClientSecret,
		Scopes:       []string{"user:email"},
		Endpoint:     github.Endpoint,
	}

	facebookOauthConfig = &oauth2.Config{
		RedirectURL:  "http://localhost:8065/facebook/callback",
		ClientID:     config.FacebookClientID,
		ClientSecret: config.FacebookClientSecret,
		Scopes:       []string{"email"},
		Endpoint:     facebook.Endpoint,
	}
}

// HandleGoogleLogin fonksiyonu, Google ile giriş yapmayı başlatır.
func HandleGoogleLogin(w http.ResponseWriter, r *http.Request) {
	// Registerda registering değişkeni true olarak ayarlanır.
	registering = false // Kullanılma amacı giriş yapma (login) ve kaydolma (register) işlemleri arasında ayrım yapmaktır.
	oauthStateStringGoogle = generateNonce()
	url := googleOauthConfig.AuthCodeURL(oauthStateStringGoogle, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func generateNonce() string {
	// Rastgele 32 byte veri oluşturulur
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		// Eğer rastgele veri oluşturulurken hata alınırsa panic ile program sonlandırılır.
		panic(err)
	}
	// Oluşturulan byte slice base64 URL encoding ile string formatına dönüştürülür ve döndürülür
	return base64.URLEncoding.EncodeToString(b)
}

// HandleGoogleRegister fonksiyonu, Google ile kaydolmayı başlatır.
func HandleGoogleRegister(w http.ResponseWriter, r *http.Request) {
	registering = true
	oauthStateStringGoogle = generateNonce()
	url := googleOauthConfig.AuthCodeURL(oauthStateStringGoogle, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

// Ana sayfayı görüntüler.
func HomeHandler(w http.ResponseWriter, r *http.Request) {
	session, err := datahandlers.GetSession(r)
	if err != nil {
		utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
		return
	}

	searchQuery := r.URL.Query().Get("search")
	category := r.URL.Query().Get("category")
	filter := r.URL.Query().Get("filter")

	posts, err := getFilteredPosts(searchQuery, category, filter, nil)
	if err != nil {
		utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Kullanıcının admin olup olmadığını kontrol et
	isAdmin := false
	if session != nil {
		isAdmin, err = CheckIfAdmin(int64(session.UserID))
		if err != nil {
			utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
			return
		}
	}

	// Şablon verilerini oluştur
	data := AdminTemplateData{
		Posts:    posts,
		LoggedIn: session != nil,
		IsAdmin:  isAdmin,
	}

	// Şablonu işleme
	tmpl, err := template.ParseFiles("templates/index.html")
	if err != nil {
		utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
		return
	}

	err = tmpl.Execute(w, data)
	if err != nil {
		utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
	}
}

// Verilen filtrelere (arama sorgusu, kategori, filtre türü, kullanıcı ID'si) göre gönderileri veritabanından çeker.
func getFilteredPosts(searchQuery, category, filter string, userID *int) ([]Post, error) {
	query := `SELECT posts.id, posts.user_id, posts.title, posts.content, posts.categories, posts.created_at, users.username,
                     COALESCE(SUM(CASE WHEN votes.vote_type = 1 THEN 1 ELSE 0 END), 0) AS like_count,
                     COALESCE(SUM(CASE WHEN votes.vote_type = -1 THEN 1 ELSE 0 END), 0) AS dislike_count,
                     (SELECT COUNT(*) FROM comments WHERE comments.post_id = posts.id AND comments.deleted = 0) AS comment_count
              FROM posts
              JOIN users ON posts.user_id = users.id
              LEFT JOIN votes ON votes.post_id = posts.id
              WHERE posts.deleted = 0`

	args := []interface{}{}  // Sorgu parametreleri için
	conditions := []string{} // Filtreleme koşulları için

	if searchQuery != "" {
		conditions = append(conditions, "(posts.title LIKE ? OR posts.content LIKE ?)")
		searchTerm := "%" + searchQuery + "%"
		args = append(args, searchTerm, searchTerm)
	}

	if category != "" {
		conditions = append(conditions, "posts.categories LIKE ?")
		categoryTerm := "%" + category + "%"
		args = append(args, categoryTerm)
	}

	if userID != nil {
		conditions = append(conditions, "(posts.user_id = ? OR posts.id IN (SELECT post_id FROM likes WHERE user_id = ?))")
		args = append(args, *userID, *userID)
	}

	if len(conditions) > 0 {
		query += " AND " + strings.Join(conditions, " AND ")
	}

	query += " GROUP BY posts.id"

	switch filter {
	case "most_liked":
		query += " ORDER BY like_count DESC"
	case "most_commented":
		query += " ORDER BY comment_count DESC"
	default:
		query += " ORDER BY posts.created_at DESC"
	}

	rows, err := datahandlers.DB.Query(query, args...)
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

func RegisterHandler(w http.ResponseWriter, r *http.Request) {
	// Check if the user is already logged in
	session, err := datahandlers.GetSession(r)
	if err == nil && session != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	// Check for email existence in the database (GET or POST request)
	errorMessages := make(map[string]string)
	email := r.FormValue("email")
	if email == "" {
		email = r.URL.Query().Get("email")
	}

	if email != "" {
		existingUser, _ := getUserByEmail(email)
		if existingUser != nil {
			errorMessages["Email"] = "Bu Email zaten kayıtlı."
		}
	}

	switch r.Method {
	case http.MethodPost:
		err := registerUser(w, r)
		if err != nil {
			// ... (handle other errors)

			// Pass specific error message to the template
			if err.Error() == "user already exists" {
				errorMessages["Email"] = "Bu Email zaten kayıtlı."
			} else if err.Error() == "username already exists" {
				errorMessages["Username"] = "Bu kullanıcı adı zaten alınmış."
			} else {
				errorMessages["Email"] = err.Error() // Generic error message
			}

			renderRegisterTemplate(w, RegisterTemplateData{ErrorMessages: errorMessages})
			return
		}
	default: // GET request
		tmpl, err := template.ParseFiles("templates/register.html")
		if err != nil {
			utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Şablon verilerini hazırla
		data := RegisterTemplateData{
			ErrorMessages: errorMessages, // Hata mesajlarını şablona aktar
			Email:         email,
		}
		err = tmpl.Execute(w, data)
		if err != nil {
			utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
		}
	}
}

func loginError(w http.ResponseWriter, r *http.Request, message string) {
	redirectURL := "/login?error=" + message
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

func registerUser(w http.ResponseWriter, r *http.Request) error {
	email := r.FormValue("email")
	googleOAuth := r.FormValue("google_oauth")

	// 1. Check if email exists (regardless of registration method)
	var existingUserID int
	err := datahandlers.DB.QueryRow("SELECT id FROM users WHERE email = ?", email).Scan(&existingUserID)
	existingUser, _ := getUserByEmail(email)
	if existingUser != nil {
		return fmt.Errorf("user already exists")
	}

	var user User
	if googleOAuth == "true" {
		// 2b. Google OAuth (New Registration)
		code := r.FormValue("code")
		token, err := googleOauthConfig.Exchange(r.Context(), code)
		if err != nil {
			return err
		}

		email, name, err := getEmailAndNameFromGoogle(token)
		if err != nil {
			return err
		}

		username := strings.ToLower(strings.ReplaceAll(name, " ", "")) + "_" + generateRandomString(5)

		user = User{
			Email:    email,
			Username: sql.NullString{String: username, Valid: true},
			Password: sql.NullString{Valid: false}, // No password for Google OAuth
		}
	} else { // Normal registration
		// 2d. Normal Registration (New Account)
		username := r.FormValue("username")
		password := r.FormValue("password")

		// Username check
		var count int
		err := datahandlers.DB.QueryRow("SELECT COUNT(*) FROM users WHERE username = ?", username).Scan(&count)
		if err != nil {
			return err
		}
		if count > 0 {
			registerError(w, r, "username already exists")
			return fmt.Errorf("username already exists")
		}

		// Password validation
		if err := validate.Var(password, "required,min=6"); err != nil {
			return err
		}

		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}

		user = User{
			Email:    email,
			Username: sql.NullString{String: username, Valid: true},
			Password: sql.NullString{String: string(hashedPassword), Valid: true},
		}
	}

	// 3. Save the user
	err = saveUser(&user)
	if err != nil {
		return err
	}

	// 4. Create session and redirect
	sessionToken, err := createSession(int64(user.ID))
	if err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    sessionToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   true, // If using HTTPS
	})

	http.Redirect(w, r, "/myprofil", http.StatusSeeOther) // Redirect to profile page on successful registration

	return nil // Successful registration
}

func registerError(w http.ResponseWriter, r *http.Request, s string) {
	panic("unimplemented")
}

func saveUser(user *User) error {
	_, err := datahandlers.DB.Exec("INSERT INTO users (email, username, password) VALUES (?, ?, ?)", user.Email, user.Username, user.Password)
	return err
}

func getUserByEmail(email string) (*User, error) {
	var user User
	err := datahandlers.DB.QueryRow("SELECT id, email, username, password FROM users WHERE email = ?", email).Scan(&user.ID, &user.Email, &user.Username, &user.Password)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // User not found
		}
		return nil, err // Database error
	}
	return &user, nil
}

func getErrorMessage(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "This field is required"
	case "email":
		return "Invalid email address"
	default:
		return "Validation error" // Generic error message
	}
}

func getGoogleUserByEmail(email string) (int64, error) {
	var userID int64
	// Check for null password (indicating Google registration)
	err := datahandlers.DB.QueryRow("SELECT id FROM users WHERE email = ? AND password IS NULL", email).Scan(&userID)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil // User not found
		}
		return 0, err // Database error
	}
	return userID, nil
}

// Kayıt formunu göstermek için HTML şablonunu render eder.
func renderRegisterTemplate(w http.ResponseWriter, data RegisterTemplateData) {
	tmpl, err := template.ParseFiles("templates/register.html")
	if err != nil {
		utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
		return
	}

	err = tmpl.Execute(w, data)
	if err != nil {
		utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
	}
}

// Kullanıcı oturum açma işlemini işler.
// Kullanıcı oturum açma işlemini işler.
func LoginHandler(w http.ResponseWriter, r *http.Request) {
	// Kullanıcı zaten giriş yapmış mı kontrol et
	session, err := datahandlers.GetSession(r)
	if err == nil && session != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	// Hata mesajını taşımak için bir yapı oluşturun
	tmplData := struct {
		Error string
	}{}

	if r.Method == http.MethodPost {
		email := r.FormValue("email")
		password := r.FormValue("password")
		googleOAuth := r.FormValue("google_oauth")

		// Kullanıcının banlı olup olmadığını kontrol et
		banned, err := checkIfBanned(email)
		if err != nil {
			utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
			return
		}
		if banned {
			tmplData.Error = "Bu kullanıcı banlanmış."
			tmpl, err := template.ParseFiles("templates/login.html")
			if err != nil {
				utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
				return
			}
			err = tmpl.Execute(w, tmplData)
			if err != nil {
				utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
			}
			return
		}

		if googleOAuth == "true" {
			// Google OAuth ile giriş yapma işlemleri
			code := r.FormValue("code")
			token, err := googleOauthConfig.Exchange(r.Context(), code)
			if err != nil {
				http.Error(w, "Failed to exchange token", http.StatusInternalServerError)
				return
			}

			email, _, err := getEmailAndNameFromGoogle(token)
			if err != nil {
				http.Error(w, "Failed to get user info from Google", http.StatusInternalServerError)
				return
			}

			// Google ile kayıtlı kullanıcıyı bul
			userID, err := getGoogleUserByEmail(email)
			if err != nil {
				if err == sql.ErrNoRows {
					utils.HandleErr(w, err, "User not found. Please register first.", http.StatusUnauthorized)
					return
				}
				utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
				return
			}

			sessionToken, err := createSession(userID)
			if err != nil {
				http.Error(w, "Failed to create session", http.StatusInternalServerError)
				return
			}

			http.SetCookie(w, &http.Cookie{
				Name:     "session_token",
				Value:    sessionToken,
				Path:     "/",
				HttpOnly: true,
			})

			http.Redirect(w, r, "/myprofil", http.StatusSeeOther)
			return

		} else {
			// Normal giriş yapma işlemleri
			var id int
			var hashedPassword string
			err := datahandlers.DB.QueryRow("SELECT id, password FROM users WHERE email = ?", email).Scan(&id, &hashedPassword)
			if err != nil {
				if err == sql.ErrNoRows {
					tmplData.Error = "Geçersiz e-posta veya şifre"
				} else {
					utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
				}
				tmpl, err := template.ParseFiles("templates/login.html")
				if err != nil {
					utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
					return
				}
				err = tmpl.Execute(w, tmplData)
				if err != nil {
					utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
				}
				return
			}

			err = bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
			if err != nil {
				tmplData.Error = "Geçersiz e-posta veya şifre"
				tmpl, err := template.ParseFiles("templates/login.html")
				if err != nil {
					utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
					return
				}
				err = tmpl.Execute(w, tmplData)
				if err != nil {
					utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
				}
				return
			}

			sessionToken := uuid.New().String()
			expiresAt := time.Now().Add(10 * time.Minute)

			_, err = datahandlers.DB.Exec("INSERT INTO sessions (id, user_id, expiry) VALUES (?, ?, ?)", sessionToken, id, expiresAt)
			if err != nil {
				utils.HandleErr(w, err, "Session creation failed", http.StatusInternalServerError)
				return
			}

			http.SetCookie(w, &http.Cookie{
				Name:     "session_token",
				Value:    sessionToken,
				Expires:  expiresAt,
				HttpOnly: true,
				Secure:   true,
			})

			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
	}

	// Query parametrelerinden hata mesajını al
	if errorMessage := r.URL.Query().Get("error"); errorMessage != "" {
		tmplData.Error = errorMessage
	}

	tmpl, err := template.ParseFiles("templates/login.html")
	if err != nil {
		utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Hata mesajını şablona geçirerek render et
	err = tmpl.Execute(w, tmplData)
	if err != nil {
		utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
	}
}

// Kullanıcının oturumunu kapatır.
func LogoutHandler(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session_token")
	if err != nil {
		if err == http.ErrNoCookie {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
		return
	}

	sessionToken := cookie.Value
	_, err = datahandlers.DB.Exec("DELETE FROM sessions WHERE id = ?", sessionToken)
	if err != nil {
		utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:    "session_token",
		Value:   "",
		Expires: time.Now().Add(-1 * time.Second),
	})

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func HandleGitHubLogin(w http.ResponseWriter, r *http.Request) {
	// GitHub OAuth 2.0 URL'sini oluştur ve kullanıcıyı yönlendirir
	registering = false
	oauthStateStringGoogle = generateNonce()
	url := googleOauthConfig.AuthCodeURL(oauthStateStringGoogle, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

// GitHub callback işlemini gerçekleştirir
func HandleGitHubCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	token, err := githubOauthConfig.Exchange(r.Context(), code)
	if err != nil {
		http.Error(w, "Failed to exchange token", http.StatusInternalServerError)
		return
	}

	email, name, err := getEmailAndNameFromGitHub(token)
	if err != nil {
		http.Error(w, "Failed to get user info from GitHub", http.StatusInternalServerError)
		return
	}
	// Kullanıcı adını oluştur ve kullanıcıyı kaydet veya mevcut kullanıcıyı getir
	username := strings.ToLower(strings.ReplaceAll(name, " ", "")) + "_" + generateRandomString(5)

	if registering {
		// Kayıt işlemi
		user, _ := getUserByEmail(email)
		if user != nil {
			tmpl, err := template.ParseFiles("templates/register.html")
			if err != nil {
				utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
				return
			}

			data := RegisterTemplateData{Email: email}
			data.ErrorMessages = map[string]string{"Email": "Bu Email zaten kayıtlı."}

			// Şablonu işleyerek yanıtı gönder
			err = tmpl.Execute(w, data)
			if err != nil {
				utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
			}
		} else {
			userId, _ := getOrCreateUser(email, username)

			sessionToken, _ := createSession(userId)

			http.SetCookie(w, &http.Cookie{
				Name:     "session_token",
				Value:    sessionToken,
				Path:     "/",
				HttpOnly: true,
			})
			// Kayıt başarılı, kullanıcıyı profil sayfasına yönlendir
			http.Redirect(w, r, "/myprofil", http.StatusTemporaryRedirect)
		}

	} else {
		// Oturum açma işlemi

		// Kullanıcıyı e-posta ile veritabanında bul
		var userID int
		err = datahandlers.DB.QueryRow("SELECT id FROM users WHERE email = ?", email).Scan(&userID)
		if err != nil {
			if err == sql.ErrNoRows {
				// Kullanıcı bulunamadı, hata mesajı göster
				utils.HandleErr(w, err, "Kullanıcı bulunamadı. Lütfen önce kaydolun.", http.StatusUnauthorized)
			} else {
				// Veritabanı hatası
				utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
			}
			return
		}

		// Oturum oluştur
		sessionToken, err := createSession(int64(userID))
		if err != nil {
			http.Error(w, "Oturum oluşturulamadı.", http.StatusInternalServerError)
			return
		}

		// Tarayıcıya oturum çerezi gönder
		http.SetCookie(w, &http.Cookie{
			Name:     "session_token",
			Value:    sessionToken,
			Path:     "/",
			HttpOnly: true,
		})

		// Oturum açma başarılı, kullanıcıyı ana sayfaya yönlendir
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

// GitHub access token'ı kullanarak kullanıcı bilgilerini alır
func getEmailAndNameFromGitHub(token *oauth2.Token) (string, string, error) {
	client := githubOauthConfig.Client(oauth2.NoContext, token)
	resp, err := client.Get("https://api.github.com/user")
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	// GitHub API'sinden dönen JSON'ı ayrıştır
	var userInfo struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return "", "", err
	}

	// GitHub email adresini ayrı bir endpoint'den almak gerekiyor.
	if userInfo.Email == "" {
		emailResp, err := client.Get("https://api.github.com/user/emails")
		if err != nil {
			return "", "", err
		}
		defer emailResp.Body.Close()

		var emails []struct {
			Email    string `json:"email"`
			Primary  bool   `json:"primary"`
			Verified bool   `json:"verified"`
		}
		if err := json.NewDecoder(emailResp.Body).Decode(&emails); err != nil {
			return "", "", err
		}
		for _, e := range emails {
			if e.Primary && e.Verified {
				userInfo.Email = e.Email
				break
			}
		}
	}

	return userInfo.Email, userInfo.Name, nil
}

// HandleGoogleCallback fonksiyonu, Google'dan gelen callback isteğini işler.
func HandleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	// CSRF (Çapraz Site İstek Sahteciliği) koruması
	if r.FormValue("state") != oauthStateStringGoogle {
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}
	// Google'dan dönen yetkilendirme kodunu al
	code := r.URL.Query().Get("code")

	// Yetkilendirme kodunu access token ile değiştir
	token, err := googleOauthConfig.Exchange(r.Context(), code)
	if err != nil {
		http.Error(w, "Token değişimi başarısız oldu.", http.StatusInternalServerError)
		return
	}

	// Access token ile Google'dan kullanıcı bilgilerini al
	email, name, err := getEmailAndNameFromGoogle(token)
	if err != nil {
		http.Error(w, "Google'dan kullanıcı bilgileri alınamadı.", http.StatusInternalServerError)
		return
	}

	// Kullanıcı adını oluştur (boşlukları kaldır ve küçük harfe çevir)
	username := strings.ToLower(strings.ReplaceAll(name, " ", ""))

	if registering {
		// Kayıt işlemi
		user, _ := getUserByEmail(email)
		if user != nil {
			tmpl, err := template.ParseFiles("templates/register.html")
			if err != nil {
				utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
				return
			}

			data := RegisterTemplateData{Email: email}
			data.ErrorMessages = map[string]string{"Email": "Bu Email zaten kayıtlı."}

			// Şablonu işleyerek yanıtı gönder
			err = tmpl.Execute(w, data)
			if err != nil {
				utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
			}
		} else {
			userId, _ := getOrCreateUser(email, username)

			sessionToken, _ := createSession(userId)

			http.SetCookie(w, &http.Cookie{
				Name:     "session_token",
				Value:    sessionToken,
				Path:     "/",
				HttpOnly: true,
			})
			// Kayıt başarılı, kullanıcıyı profil sayfasına yönlendir
			http.Redirect(w, r, "/myprofil", http.StatusTemporaryRedirect)
		}

	} else {
		// Oturum açma işlemi

		// Kullanıcıyı e-posta ile veritabanında bul
		var userID int
		err = datahandlers.DB.QueryRow("SELECT id FROM users WHERE email = ?", email).Scan(&userID)
		if err != nil {
			if err == sql.ErrNoRows {
				// Kullanıcı bulunamadı, hata mesajı göster
				utils.HandleErr(w, err, "Kullanıcı bulunamadı. Lütfen önce kaydolun.", http.StatusUnauthorized)
			} else {
				// Veritabanı hatası
				utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
			}
			return
		}

		// Oturum oluştur
		sessionToken, err := createSession(int64(userID))
		if err != nil {
			http.Error(w, "Oturum oluşturulamadı.", http.StatusInternalServerError)
			return
		}

		// Tarayıcıya oturum çerezi gönder
		http.SetCookie(w, &http.Cookie{
			Name:     "session_token",
			Value:    sessionToken,
			Path:     "/",
			HttpOnly: true,
		})

		// Oturum açma başarılı, kullanıcıyı ana sayfaya yönlendir
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

func getEmailAndNameFromGoogle(token *oauth2.Token) (string, string, error) {
	client := googleOauthConfig.Client(oauth2.NoContext, token)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	var userInfo struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return "", "", err
	}

	return userInfo.Email, userInfo.Name, nil
}

// getOrCreateUser fonksiyonu, e-posta adresine göre kullanıcıyı bulur veya yeni bir kullanıcı oluşturur.
func getOrCreateUser(email, username string) (int64, error) {
	var userID int64
	err := datahandlers.DB.QueryRow("SELECT id FROM users WHERE email = ?", email).Scan(&userID)
	if err != nil {
		if err == sql.ErrNoRows {
			// If user doesn't exist, create a new one
			res, err := datahandlers.DB.Exec("INSERT INTO users (email, username) VALUES (?, ?)", email, username)
			if err != nil {
				return 0, err
			}
			userID, err = res.LastInsertId()
			if err != nil {
				return 0, err
			}
		} else {
			// Other database errors
			return 0, err
		}
	}

	return userID, nil
}

// createSession fonksiyonu, kullanıcı için yeni bir oturum oluşturur.
func createSession(userID int64) (string, error) {
	sessionToken := fmt.Sprintf("session-%d-%d", userID, time.Now().UnixNano())
	expiry := time.Now().Add(10 * time.Minute)
	_, err := datahandlers.DB.Exec("INSERT INTO sessions (id, user_id, expiry) VALUES (?, ?, ?)", sessionToken, userID, expiry)
	if err != nil {
		return "", err
	}
	return sessionToken, nil
}

func SifreUnutHandler(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFiles("templates/sifreunut.html")
	if err != nil {
		utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
		return
	}

	err = tmpl.Execute(w, nil)
	if err != nil {
		utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
	}
}

func HandleFacebookLogin(w http.ResponseWriter, r *http.Request) {
	url := facebookOauthConfig.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func HandleFacebookCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	token, err := facebookOauthConfig.Exchange(r.Context(), code)
	if err != nil {
		http.Error(w, "Failed to exchange token", http.StatusInternalServerError)
		return
	}

	email, name, err := getEmailAndNameFromFacebook(token)
	if err != nil {
		http.Error(w, "Failed to get user info from Facebook", http.StatusInternalServerError)
		return
	}

	username := strings.ToLower(strings.ReplaceAll(name, " ", "")) + "_" + generateRandomString(5)

	userID, err := getOrCreateUser(email, username)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to save user info: %v", err), http.StatusInternalServerError)
		return
	}

	sessionToken, err := createSession(userID)
	if err != nil {
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    sessionToken,
		Path:     "/",
		HttpOnly: true,
	})

	http.Redirect(w, r, "/myprofil", http.StatusTemporaryRedirect)
}

// Facebook profilinden e-posta adresini ve adını alır.
func getEmailAndNameFromFacebook(token *oauth2.Token) (string, string, error) {
	client := facebookOauthConfig.Client(oauth2.NoContext, token)
	resp, err := client.Get("https://graph.facebook.com/me?fields=id,name,email")
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	var userInfo struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return "", "", err
	}

	return userInfo.Email, userInfo.Name, nil
}

const maxUploadSize = 20 * 1024 * 1024 // 20 MB

func UploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		http.Error(w, "The uploaded file is too big. Please choose an file that's less than 20MB in size", http.StatusBadRequest)
		return
	}

	file, handler, err := r.FormFile("file")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Dosya tipi kontrolü
	allowedExtensions := map[string]bool{
		".jpg":  true,
		".jpeg": true,
		".png":  true,
		".gif":  true,
	}
	ext := filepath.Ext(handler.Filename)
	if !allowedExtensions[ext] {
		http.Error(w, "The provided file format is not allowed. Please upload a JPEG, PNG, or GIF image", http.StatusBadRequest)
		return
	}

	// Dosyayı kaydet
	f, err := os.OpenFile("./uploads/"+handler.Filename, os.O_WRONLY|os.O_CREATE, 0o666)
	if err != nil {
		http.Error(w, "Internal Server Error: Dosya kaydedilemedi.", http.StatusInternalServerError)
		log.Println("Error saving file:", err) // Loglara hata mesajını yaz
		return
	}
	defer f.Close()
	io.Copy(f, file)

	fmt.Fprintf(w, "File uploaded successfully: %s", handler.Filename)
}

func AdminHandler(w http.ResponseWriter, r *http.Request) {
	session, err := datahandlers.GetSession(r)
	if err != nil || session == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Admin olup olmadığını kontrol et
	isAdmin, err := CheckIfAdmin(int64(session.UserID)) // int dönüşümü
	if err != nil || !isAdmin {
		http.Redirect(w, r, "/", http.StatusForbidden)
		return
	}

	// Veritabanından kullanıcılar ve gönderiler al
	users, err := getUsers()
	if err != nil {
		utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
		return
	}

	posts, err := getPosts()
	if err != nil {
		utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Admin sayfası şablonunu işle
	tmpl, err := template.ParseFiles("templates/admin.html")
	if err != nil {
		utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
		return
	}

	data := AdminTemplateData{
		Users:    users,
		Posts:    posts,
		LoggedIn: session != nil,
		IsAdmin:  isAdmin,
	}

	err = tmpl.Execute(w, data)
	if err != nil {
		utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
	}
}

func CheckIfAdmin(userID int64) (bool, error) {
	var role string
	err := datahandlers.DB.QueryRow("SELECT role FROM users WHERE id = ?", userID).Scan(&role)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil // Kullanıcı bulunamadıysa admin değil
		}
		return false, err // Veritabanı hatası
	}
	return role == "admin", nil
}

func DeleteUserHandler(w http.ResponseWriter, r *http.Request) {
	userIDStr := strings.TrimPrefix(r.URL.Path, "/users/delete/")
	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		utils.HandleErr(w, err, "Invalid user ID", http.StatusBadRequest)
		return
	}

	// Kullanıcı bilgilerini alın
	user, err := getUserByID(userID)
	if err != nil {
		utils.HandleErr(w, err, "User not found", http.StatusNotFound)
		return
	}

	// Transaction başlat
	tx, err := datahandlers.DB.Begin()
	if err != nil {
		utils.HandleErr(w, err, "Failed to begin transaction", http.StatusInternalServerError)
		return
	}

	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			panic(r)
		}
	}()

	// Kullanıcıyı users tablosundan sil
	_, err = tx.Exec("DELETE FROM users WHERE id = ?", userID)
	if err != nil {
		tx.Rollback()
		utils.HandleErr(w, err, "Failed to delete user", http.StatusInternalServerError)
		return
	}

	// Kullanıcıyı banned_users tablosuna ekle
	_, err = tx.Exec("INSERT INTO banned_users (email) VALUES (?)", user.Email)
	if err != nil {
		tx.Rollback()
		utils.HandleErr(w, err, "Failed to ban user", http.StatusInternalServerError)
		return
	}

	// Transaction'ı commit et
	err = tx.Commit()
	if err != nil {
		utils.HandleErr(w, err, "Failed to commit transaction", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func checkIfBanned(email string) (bool, error) {
	var exists bool
	err := datahandlers.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM banned_users WHERE email = ?)", email).Scan(&exists)
	return exists, err
}

// Kullanıcı bilgilerini güncelleyen handler
func UpdateUserHandler(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimPrefix(r.URL.Path, "/users/update/")

	email := r.FormValue("email")
	username := r.FormValue("username")
	role := r.FormValue("role")

	_, err := datahandlers.DB.Exec("UPDATE users SET email = ?, username = ?, role = ? WHERE id = ?", email, username, role, userID)
	if err != nil {
		utils.HandleErr(w, err, "Failed to update user", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// getUsers, tüm kullanıcıları veritabanından alır
func getUsers() ([]User, error) {
	rows, err := datahandlers.DB.Query("SELECT id, email, username, role FROM users")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var user User
		if err := rows.Scan(&user.ID, &user.Email, &user.Username, &user.Role); err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, nil
}

func EditUserHandler(w http.ResponseWriter, r *http.Request) {
	userIDStr := strings.TrimPrefix(r.URL.Path, "/users/edit/")
	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		utils.HandleErr(w, err, "Invalid user ID", http.StatusBadRequest)
		return
	}

	user, err := getUserByID(userID)
	if err != nil {
		utils.HandleErr(w, err, err.Error(), http.StatusNotFound)
		return
	}

	tmpl, err := template.ParseFiles("templates/edit_user.html")
	if err != nil {
		utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
		return
	}

	data := struct {
		User *User
	}{
		User: user,
	}

	err = tmpl.Execute(w, data)
	if err != nil {
		utils.HandleErr(w, err, "Internal server error", http.StatusInternalServerError)
	}
}

// Belirtilen kullanıcı ID'sine sahip kullanıcıyı veritabanından çeker.
func getUserByID(userID int) (*User, error) {
	var user User
	query := "SELECT id, email, username, role FROM users WHERE id = ?"
	err := datahandlers.DB.QueryRow(query, userID).Scan(&user.ID, &user.Email, &user.Username, &user.Role)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user with ID %d not found", userID)
		}
		return nil, err
	}
	return &user, nil
}

// getPosts, tüm gönderileri veritabanından alır
func getPosts() ([]Post, error) {
	rows, err := datahandlers.DB.Query("SELECT id, title, content, created_at FROM posts WHERE deleted = 0")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []Post
	for rows.Next() {
		var post Post
		if err := rows.Scan(&post.ID, &post.Title, &post.Content, &post.CreatedAt); err != nil {
			return nil, err
		}
		posts = append(posts, post)
	}
	return posts, nil
}
