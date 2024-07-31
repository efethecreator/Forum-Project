package allhandlers

import (
	"net/http"
	"strings"

	"form-project/homehandlers"
	"form-project/morehandlers"
	"form-project/posthandlers"
)

func Allhandlers() {
	// Statik Dosya Sunumu:
	http.HandleFunc("/static/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path[1:]
		if !strings.HasPrefix(path, "static/") {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, path)
	})
	http.HandleFunc("/google/register", homehandlers.HandleGoogleRegister)
	http.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir("./uploads"))))
	http.HandleFunc("/upload", homehandlers.UploadHandler)

	// Google Oturum İşlemleri:
	http.HandleFunc("/google/login", homehandlers.HandleGoogleLogin)
	http.HandleFunc("/google/callback", homehandlers.HandleGoogleCallback)

	// GitHub Oturum İşlemleri:
	http.HandleFunc("/github/login", homehandlers.HandleGitHubLogin)
	http.HandleFunc("/github/callback", homehandlers.HandleGitHubCallback)
	// Facebook Oturum İşlemleri:
	http.HandleFunc("/facebook/login", homehandlers.HandleFacebookLogin)
	http.HandleFunc("/facebook/callback", homehandlers.HandleFacebookCallback)

	// Diğer İşleyiciler:
	http.HandleFunc("/", homehandlers.HomeHandler)
	http.HandleFunc("/register", homehandlers.RegisterHandler)
	http.HandleFunc("/login", homehandlers.LoginHandler)
	http.HandleFunc("/logout", homehandlers.LogoutHandler)
	http.HandleFunc("/sifreunut", homehandlers.SifreUnutHandler)
	http.HandleFunc("/admin", homehandlers.AdminHandler)

	// Gönderi İşlemleri:
	http.HandleFunc("/createPost", posthandlers.CreatePostHandler)
	http.HandleFunc("/createComment", posthandlers.CreateCommentHandler)
	http.HandleFunc("/deletePost", posthandlers.DeletePostHandler)
	http.HandleFunc("/deleteComment", posthandlers.DeleteCommentHandler)
	http.HandleFunc("/vote", posthandlers.VoteHandler)
	http.HandleFunc("/viewPost", posthandlers.ViewPostHandler)
	http.HandleFunc("/reportPost/{id}", posthandlers.ReportPostHandler)

	// Profil İşlemleri:
	http.HandleFunc("/myprofil", morehandlers.MyProfileHandler)

	// Kullanıcı İşlemleri:
	http.HandleFunc("/users/edit/", morehandlers.EditUserHandler)     // Kullanıcı düzenleme işlemi için işleyici
	http.HandleFunc("/users/update/", homehandlers.UpdateUserHandler) // Kullanıcı güncelleme işlemi için işleyici
	http.HandleFunc("/users/delete/", homehandlers.DeleteUserHandler)
	http.HandleFunc("/posts/delete/", posthandlers.DeletePostHandler)

	http.HandleFunc("/categories/add", homehandlers.AddCategoryHandler)
	http.HandleFunc("/categories/delete/{id}", homehandlers.DeleteCategoryHandler)
}
