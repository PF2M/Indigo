// Various handlers for the various routes of Indigo.

package main

import (
	// Internals
	"bytes"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	// Externals
	"github.com/badoux/checkmail"
	"github.com/gorilla/csrf"
	"github.com/gorilla/mux"
	sessions "github.com/kataras/go-sessions"
	"github.com/lucasb-eyer/go-colorful"
	"golang.org/x/crypto/bcrypt"
)

// Accept a friend request.
func acceptFriendRequest(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	vars := mux.Vars(r)
	username := vars["username"]
	var user_id int
	var requested int
	err := db.QueryRow("SELECT id FROM users WHERE username = ?", username).Scan(&user_id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if user_id == 0 {
		http.Error(w, "That user does not exist.", http.StatusBadRequest)
		return
	}

	err = db.QueryRow("SELECT COUNT(*) FROM friend_requests WHERE request_by = ? AND request_to = ?", user_id, currentUser.ID).Scan(&requested)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if requested == 0 {
		http.Error(w, "This user has not sent you a friend request.", http.StatusBadRequest)
		return
	}

	stmt, err := db.Prepare("INSERT INTO friendships (source, target) VALUES (?, ?)")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	stmt.Exec(&user_id, &currentUser.ID)

	var conversation_id int
	err = db.QueryRow("SELECT id FROM conversations WHERE (source = ? AND target = ?) OR (source = ? AND target = ?)", user_id, currentUser.ID, currentUser.ID, user_id).Scan(&conversation_id)
	if conversation_id == 0 {
		stmt, err = db.Prepare("INSERT INTO conversations (source, target) SELECT ?, ? FROM dual WHERE NOT EXISTS (SELECT 1 FROM conversations WHERE (source = ? AND target = ?) OR (source = ? AND target = ?))")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		stmt.Exec(&user_id, &currentUser.ID, &user_id, &currentUser.ID, &currentUser.ID, &user_id)
	} else {
		stmt, err = db.Prepare("UPDATE conversations SET is_rm = 0 WHERE id = ?")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		stmt.Exec(&conversation_id)
	}

	stmt, err = db.Prepare("DELETE FROM friend_requests WHERE request_by = ? AND request_to = ?")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	stmt.Exec(&user_id, &currentUser.ID)
}

// Give a favorite to a community.
func addCommunityFavorite(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
	}

	vars := mux.Vars(r)
	community_id := vars["id"]

	var communityExists int
	err := db.QueryRow("SELECT COUNT(*) FROM communities WHERE id = ? AND rm = 0", community_id).Scan(&communityExists)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if communityExists == 0 {
		handle404(w, r)
		return
	}

	stmt, err := db.Prepare("INSERT INTO community_favorites (community, favorite_by) VALUES (?, ?)")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	stmt.Exec(&community_id, &currentUser.ID)
}

// Ban a user.
func adminBanUser(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}
	if currentUser.Level < admin.Manage.MinimumLevel {
		http.Redirect(w, r, "/", 301)
		return
	}

	length := r.FormValue("length")
	cidr := r.FormValue("cidr")
	if cidr != "1" {
		cidr = "0"
	}
	username := r.FormValue("username")
	userID := -1
	var ip string
	db.QueryRow("SELECT id, ip FROM users WHERE username = ? LIMIT 1", username).Scan(&userID, &ip)
	if userID == -1 {
		http.Error(w, "The user does not exist.", http.StatusBadRequest)
		return
	}
	if len(ip) > 0 && cidr == "1" {
		ip = getCIDR(ip)
	}
	db.Exec("REPLACE INTO bans (user, ip, cidr, until) VALUES (?, ?, ?, NOW() + INTERVAL ? DAY)", userID, ip, cidr, length)

	var msg wsMessage
	msg.Type = "refresh"
	for client := range clients {
		//clients[client].Mutex.Lock()
		if clients[client].UserID == userID {
			err := writeWs(clients[client], client, msg)
			if err != nil {
				client.Close()
				delete(clients, client)
			}
		}
		//clients[client].Mutex.Unlock()
	}

	w.Write([]byte("Success!"))
}

// Unban a user.
func adminUnbanUser(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}
	if currentUser.Level < admin.Manage.MinimumLevel {
		http.Redirect(w, r, "/", 301)
		return
	}

	username := r.FormValue("username")
	userID := -1
	var ip string
	db.QueryRow("SELECT id, ip FROM users WHERE username = ? LIMIT 1", username).Scan(&userID, &ip)
	if userID == -1 {
		http.Error(w, "The user does not exist.", http.StatusBadRequest)
		return
	}
	cidr := getCIDR(ip)
	db.Exec("DELETE FROM bans WHERE user = ? OR (cidr = 0 AND ip = ?) OR (cidr = 1 AND ip = ?)", userID, ip, cidr)
	w.Write([]byte("Success!"))
}

// Block a user.
func blockUser(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	vars := mux.Vars(r)

	username := vars["username"]

	if username != currentUser.Username {
		var user_id int
		var usern string
		db.QueryRow("SELECT id, username FROM users WHERE username = ?", username).Scan(&user_id, &usern)
		if len(usern) == 0 {
			handle404(w, r)
			return
		}

		stmt, err := db.Prepare("INSERT blocks SET source = ?, target = ?")
		if err == nil {
			// If there's no errors, we can go ahead and execute the statement.
			_, err := stmt.Exec(&currentUser.ID, &user_id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			stmt, err = db.Prepare("DELETE FROM friendships WHERE (source = ? AND target = ?) OR (source = ? AND target = ?)")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			stmt.Exec(&user_id, &currentUser.ID, &currentUser.ID, &user_id)

			stmt, err = db.Prepare("UPDATE conversations SET is_rm = 1 WHERE (source = ? AND target = ?) OR (source = ? AND target = ?)")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			stmt.Exec(&user_id, &currentUser.ID, &currentUser.ID, &user_id)

			stmt, err = db.Prepare("DELETE FROM follows WHERE (follow_to = ? AND follow_by = ?) OR (follow_to = ? AND follow_by = ?)")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			stmt.Exec(&user_id, &currentUser.ID, &currentUser.ID, &user_id)

			var msg wsMessage
			msg.Type = "block"
			msg.Content = currentUser.Username
			for client := range clients {
				//clients[client].Mutex.Lock()
				if clients[client].UserID == user_id {
					err := writeWs(clients[client], client, msg)
					if err != nil {
						client.Close()
						delete(clients, client)
					}
				}
				//clients[client].Mutex.Unlock()
			}
		}
	}
}

// Cancel a friend request.
func cancelFriendRequest(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	vars := mux.Vars(r)
	username := vars["username"]
	var user_id int

	err := db.QueryRow("SELECT id FROM users WHERE username = ?", username).Scan(&user_id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if user_id == 0 {
		http.Error(w, "That user does not exist.", http.StatusBadRequest)
		return
	}

	stmt, err := db.Prepare("DELETE FROM friend_requests WHERE request_by = ? AND request_to = ?")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	stmt.Exec(&currentUser.ID, &user_id)
}

// the handler for comment creation
func createComment(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	vars := mux.Vars(r)
	post_id := vars["id"]
	user_id := currentUser.ID
	post_type := r.FormValue("post_type")
	body := r.FormValue("body")
	painting := r.FormValue("painting")
	if post_type == "1" {
		body = painting
	}
	image := r.FormValue("image")
	attachment_type := r.FormValue("attachment_type")
	url := r.FormValue("url")
	url_type := 0
	is_spoiler := r.FormValue("is_spoiler")
	feeling := r.FormValue("feeling_id")

	// Check if a comment has been made recently.
	var post_by int
	var recent_comment int
	db.QueryRow("SELECT created_by FROM posts WHERE id = ?", post_id).Scan(&post_by)
	if currentUser.ID != post_by {
		db.QueryRow("SELECT COUNT(*) FROM comments WHERE created_by = ? AND created_at > DATE_SUB(NOW(), INTERVAL 10 SECOND)", currentUser.ID).Scan(&recent_comment)
		if recent_comment > 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			// Feel free to un-hardcode this if you want.
			w.Write([]byte("{\"success\":0,\"errors\":[{\"message\":\"You're making comments too fast, wait a few seconds and try again.\",\"error_code\":0}],\"code\":400}"))
			return
		}
	}

	if utf8.RuneCountInString(body) > 2000 {
		http.Error(w, "Your comment is too long. (2000 characters maximum)", http.StatusBadRequest)
		return
	}
	if len(body) == 0 && len(image) == 0 {
		http.Error(w, "Your comment is empty.", http.StatusBadRequest)
		return
	}
	if len(image) > 0 {
		imageURL := ""
		db.QueryRow("SELECT value FROM images WHERE id = ?", image).Scan(&imageURL)
		if len(imageURL) == 0 {
			http.Error(w, "Invalid image.", http.StatusBadRequest)
			return
		}
		image = imageURL
	}
	if len(attachment_type) == 0 {
		attachment_type = "0"
	}
	if len(is_spoiler) == 0 {
		is_spoiler = "0"
	}

	if len(body) > 0 {
		matched := youtube.FindAllStringSubmatch(body, 1)
		if len(matched) > 0 {
			url = matched[0][1]
			url_type = 1
		} else {
			matched = spotify.FindAllStringSubmatch(body, 1)
			if len(matched) > 0 {
				url = matched[0][1]
				url_type = 2
			} else {
				matched = soundcloud.FindAllStringSubmatch(body, 1)
				if len(matched) > 0 {
					url = "https://" + matched[0][0]
					url_type = 3
				}
			}
		}
	}
	if len(post_type) == 0 {
		post_type = "0"
	} else if post_type == "1" {
		if len(painting) == 0 {
			http.Error(w, "You must add a drawing.", http.StatusBadRequest)
			return
		}
	} else if post_type != "0" {
		http.Error(w, "Invalid post type.", http.StatusBadRequest)
		return
	}

	postedBy := 0
	db.QueryRow("SELECT created_by FROM posts WHERE posts.id = ?", post_id).Scan(&postedBy)
	if postedBy == 0 {
		http.Error(w, "That post does not exist.", http.StatusBadRequest)
		return
	}
	if checkIfEitherBlocked(postedBy, currentUser.ID) && currentUser.Level == 0 {
		http.Error(w, "You're not allowed to do that.", http.StatusForbidden)
		return
	}

	stmt, err := db.Prepare("INSERT comments SET created_by = ?, post = ?, body = ?, image = ?, attachment_type = ?, url = ?, url_type = ?, is_spoiler = ?, post_type = ?, feeling = ?")
	if err == nil {
		// If there's no errors, we can go ahead and execute the statement.
		_, err := stmt.Exec(currentUser.ID, post_id, body, image, attachment_type, url, url_type, is_spoiler, post_type, feeling)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var comments = comment{}
		var timestamp time.Time
		var role int

		db.QueryRow("SELECT comments.id, created_by, created_at, feeling, body, image, attachment_type, is_spoiler, post_type, url, url_type, username, nickname, avatar, has_mh, online, hide_online, color, role FROM comments LEFT JOIN users ON users.id = created_by WHERE created_by = ? ORDER BY created_at DESC LIMIT 1", currentUser.ID).Scan(&comments.ID, &comments.CreatedBy, &timestamp, &comments.Feeling, &comments.BodyText, &comments.Image, &comments.AttachmentType, &comments.IsSpoiler, &comments.PostType, &comments.URL, &comments.URLType, &comments.CommenterUsername, &comments.CommenterNickname, &comments.CommenterIcon, &comments.CommenterHasMii, &comments.CommenterOnline, &comments.CommenterHideOnline, &comments.CommenterColor, &role)

		comments.CommenterIcon = getAvatar(comments.CommenterIcon, comments.CommenterHasMii, comments.Feeling)
		if role > 0 {
			comments.CommenterRoleImage = getRoleImage(role)
		}

		comments.CreatedAt = humanTiming(timestamp, currentUser.Timezone)
		comments.CreatedAtUnix = timestamp.Unix()
		comments.Body = parseBody(comments.BodyText, false, true)

		comments.ByMii = true
		var data = map[string]interface{}{
			"CanYeah": false,
			"Comment": comments,
		}

		data["ByMe"] = currentUser.ID == post_by
		if data["ByMe"] == true {
			notif_getcomments, _ := db.Query("SELECT created_by FROM comments WHERE post = ? AND created_by != ? AND is_rm = 0 GROUP BY created_by", &post_id, &user_id)
			var notif_comment_by int

			for notif_getcomments.Next() {
				notif_getcomments.Scan(&notif_comment_by)

				createNotif(notif_comment_by, 3, post_id, currentUser.ID)
			}
			notif_getcomments.Close()
		} else {
			createNotif(post_by, 2, post_id, currentUser.ID)
		}

		err = templates.ExecuteTemplate(w, "create_comment.html", data)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

		var commentTpl bytes.Buffer
		var commentPreviewTpl bytes.Buffer
		comments.ByMii = false
		data["CanYeah"] = true
		templates.ExecuteTemplate(&commentTpl, "create_comment.html", data)
		data = map[string]interface{}{
			"CommentPreview": comments,
		}
		templates.ExecuteTemplate(&commentPreviewTpl, "render_comment_preview.html", data)

		var msg wsMessage
		var community_id string

		db.QueryRow("SELECT community_id FROM posts WHERE id = ?", post_id).Scan(&community_id)

		for client := range clients {
			//clients[client].Mutex.Lock()
			if (!checkIfEitherBlocked(clients[client].UserID, comments.CreatedBy) || clients[client].Level > 0) && !inForbiddenKeywords(body, clients[client].UserID) {
				if clients[client].OnPage == "/posts/"+post_id && clients[client].UserID != comments.CreatedBy {
					msg.Type = "comment"
					msg.Content = commentTpl.String()
					err := writeWs(clients[client], client, msg)
					if err != nil {
						client.Close()
						delete(clients, client)
					}
				} else if clients[client].OnPage == "/communities/"+community_id && is_spoiler == "0" {
					msg.Type = "commentPreview"
					msg.ID = post_id
					msg.Content = commentPreviewTpl.String()
					err := writeWs(clients[client], client, msg)
					if err != nil {
						client.Close()
						delete(clients, client)
					}
				}
			}
			//clients[client].Mutex.Unlock()
		}
	}
}

// Give a Yeah to a comment.
func createCommentYeah(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	vars := mux.Vars(r)

	comment_id := vars["id"]
	user_id := currentUser.ID

	var comment_by int
	var post_id string
	var yeah_exists int
	var feeling int

	db.QueryRow("SELECT created_by, post, feeling FROM comments WHERE id = ?", comment_id).Scan(&comment_by, &post_id, &feeling)

	// Check if the comment exists first.
	if comment_by != 0 {
		db.QueryRow("SELECT id FROM yeahs WHERE yeah_post = ? AND yeah_by = ? AND on_comment = 1", comment_id, user_id).Scan(&yeah_exists)
		if yeah_exists != 0 {
			return
		}

		if checkIfCanYeah(currentUser, comment_by) {
			stmt, err := db.Prepare("INSERT yeahs SET yeah_post = ?, yeah_by = ?, on_comment = 1")
			if err == nil {
				// If there's no errors, we can go ahead and execute the statement.
				_, err := stmt.Exec(&comment_id, &user_id)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				} else {
					createNotif(comment_by, 1, comment_id, user_id)

					var msg wsMessage
					var yeahs = yeah{}

					db.QueryRow("SELECT yeahs.id, username, avatar FROM yeahs LEFT JOIN users ON users.id = yeah_by WHERE yeah_by = ? ORDER BY yeahs.id DESC LIMIT 1", user_id).Scan(&yeahs.ID, &yeahs.Username, &yeahs.Avatar)

					msg.Type = "commentYeah"
					msg.ID = comment_id
					mariosPrincessSex := "\">"
					if len(currentUser.Role.Image) > 0 {
						mariosPrincessSex = fmt.Sprintf(" official-user\"><img src=\"%s\" class=\"official-tag\">", currentUser.Role.Image)
					}
					// "I don't think we need a separate template for such a small amount of HTML." -the person who originally wrote this function
					msg.Content = fmt.Sprintf("<a href=\"/users/%s\" id=\"%d\" class=\"post-permalink-feeling-icon%s<img src=\"%s\" class=\"user-icon\"></a>", yeahs.Username, yeahs.ID, html.EscapeString(mariosPrincessSex), getAvatar(yeahs.Avatar, yeahs.HasMii, feeling))

					for client := range clients {
						//clients[client].Mutex.Lock()
						if (clients[client].OnPage == "/posts/"+post_id || clients[client].OnPage == "/comments/"+comment_id) && clients[client].UserID != user_id {
							err := writeWs(clients[client], client, msg)
							if err != nil {
								client.Close()
								delete(clients, client)
							}
						}
						//clients[client].Mutex.Unlock()
					}
				}
			}
		}
	}
}

// Follow a user.
func createFollow(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	vars := mux.Vars(r)

	username := vars["username"]
	current_username := currentUser.Username

	if username != current_username {
		var user_id int
		var usern string
		db.QueryRow("SELECT id, username FROM users WHERE username = ?", username).Scan(&user_id, &usern)
		if len(usern) == 0 {
			handle404(w, r)
			return
		}

		if checkIfEitherBlocked(user_id, currentUser.ID) && currentUser.Level == 0 {
			http.Error(w, "You're not allowed to do that.", http.StatusBadRequest)
			return
		}

		stmt, err := db.Prepare("INSERT follows SET follow_to = ?, follow_by = ?")
		if err == nil {
			// If there's no errors, we can go ahead and execute the statement.
			_, err := stmt.Exec(&user_id, &currentUser.ID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			createNotif(user_id, 4, "", currentUser.ID)

			// This is necessary for the Miiverse client-side scripts.
			w.Header().Add("Content-Type", "application/json")
			fmt.Fprint(w, "{\"following_count\":1}")

			var msg wsMessage
			msg.Type = "follow"

			for client := range clients {
				//clients[client].Mutex.Lock()
				if strings.HasPrefix(clients[client].OnPage, "/users/"+username) {
					err := writeWs(clients[client], client, msg)
					if err != nil {
						client.Close()
						delete(clients, client)
					}
				}
				//clients[client].Mutex.Unlock()
			}
		}
	}
}

// Create a group chat.
func createGroupChat(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	var users []int
	for i := 1; i <= 10; i++ {
		username := r.FormValue("user" + strconv.Itoa(i))
		if len(username) > 0 {
			var id int
			var group_permissions int
			db.QueryRow("SELECT id, group_permissions FROM users WHERE username = ?", username).Scan(&id, &group_permissions)
			if id == 0 {
				http.Error(w, "The user "+username+" does not exist.", http.StatusBadRequest)
				return
			}
			if group_permissions == 1 {
				var followCount int
				db.QueryRow("SELECT COUNT(*) FROM follows WHERE follow_to = ? AND follow_by = ?", currentUser.ID, id).Scan(&followCount)
				if followCount == 0 {
					http.Error(w, "The user "+username+" does not allow you to add them to chat groups.", http.StatusBadRequest)
					return
				}
			}
			var friendCount int
			db.QueryRow("SELECT COUNT(*) FROM friendships WHERE (source = ? AND target = ?) OR (source = ? AND target = ?)", id, currentUser.ID, currentUser.ID, id).Scan(&friendCount)
			if friendCount == 0 {
				http.Error(w, "The user "+username+" is not on your friend list.", http.StatusBadRequest)
				return
			}

			users = append(users, id)
		}
	}
	users = append(users, currentUser.ID)

	stmt, err := db.Prepare("INSERT INTO conversations (source, target) VALUES (?, 0)")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	stmt.Exec(&currentUser.ID)
	var conversationID int
	db.QueryRow("SELECT id FROM conversations WHERE source = ? AND target = 0 ORDER BY id DESC", currentUser.ID).Scan(&conversationID)
	for _, user := range users {
		stmt, err = db.Prepare("INSERT INTO group_members (user, conversation) VALUES (?, ?)")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		stmt.Exec(&user, &conversationID)
	}

	http.Redirect(w, r, "/conversations/"+strconv.Itoa(conversationID), 301)
}

// Create a post.
func createPost(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	user_id := currentUser.ID
	community_id := r.FormValue("community")
	post_type := r.FormValue("post_type")
	body := r.FormValue("body")
	painting := r.FormValue("painting")
	if post_type == "1" {
		body = painting
	}
	image := r.FormValue("image")
	attachment_type := r.FormValue("attachment_type")
	url := ""
	url_type := 0
	is_spoiler := r.FormValue("is_spoiler")
	feeling := r.FormValue("feeling_id")
	privacy := r.FormValue("privacy")
	repost := r.FormValue("repost")

	// Check if a post has been made recently.
	var recent_post int
	db.QueryRow("SELECT id FROM posts WHERE created_by = ? AND created_at > DATE_SUB(NOW(), INTERVAL 10 SECOND)", user_id).Scan(&recent_post)
	if recent_post != 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		// Feel free to un-hardcode this if you want.
		w.Write([]byte("{\"success\":0,\"errors\":[{\"message\":\"You're making posts too fast, wait a few seconds and try again.\",\"error_code\":0}],\"code\":400}"))
		return
	}

	if len(community_id) == 0 {
		http.Error(w, "You must specify a community.", http.StatusBadRequest)
		return
	}
	var communityCount int
	err = db.QueryRow("SELECT COUNT(*) FROM communities WHERE id = ? AND (rm = 0 OR id = 0) AND permissions <= ? LIMIT 1", community_id, currentUser.Level).Scan(&communityCount)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if communityCount == 0 {
		http.Error(w, "The community could not be found.", http.StatusBadRequest)
		return
	}
	if utf8.RuneCountInString(body) > 2000 {
		http.Error(w, "Your post is too long. (2000 characters maximum)", http.StatusBadRequest)
		return
	}
	if len(body) == 0 && len(image) == 0 && len(repost) == 0 {
		http.Error(w, "Your post is empty.", http.StatusBadRequest)
		return
	}
	if len(image) > 0 {
		imageURL := ""
		db.QueryRow("SELECT value FROM images WHERE id = ?", image).Scan(&imageURL)
		if len(imageURL) == 0 {
			http.Error(w, "Invalid image.", http.StatusBadRequest)
			return
		}
		image = imageURL
	}
	if len(attachment_type) == 0 {
		attachment_type = "0"
	}
	if is_spoiler != "1" {
		is_spoiler = "0"
	}
	if len(privacy) != 1 {
		privacy = "0"
	}
	if len(repost) == 0 {
		repost = "0"
	} else {
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM posts LEFT JOIN users ON users.id = created_by WHERE posts.id = ? AND is_rm = 0 AND is_rm_by_admin = 0 AND users.id NOT IN (SELECT if(source = ?, target, source) FROM blocks WHERE (source = ? AND target = users.id) OR (source = users.id AND target = ?)) AND IF(created_by = ?, true, LOWER(body) NOT REGEXP LOWER(?)) AND (privacy = 0 OR (privacy IN (1, 2, 3, 4) AND (SELECT COUNT(*) FROM friendships WHERE source = ? AND target = created_by OR source = created_by AND target = ? LIMIT 1) = 1) OR (privacy IN (1, 3, 5, 6) AND (SELECT COUNT(*) FROM follows WHERE follow_to = created_by AND follow_by = ? LIMIT 1) = 1) OR (privacy IN (1, 2, 5, 7) AND (SELECT COUNT(*) FROM follows WHERE follow_to = ? AND follow_by = created_by) = 1) OR (privacy = 8 AND ? > 0) OR created_by = ?) LIMIT 1", repost, currentUser.ID, currentUser.ID, currentUser.ID, currentUser.ID, escapeForbiddenKeywords(currentUser.ForbiddenKeywords), currentUser.ID, currentUser.ID, currentUser.ID, currentUser.ID, currentUser.Level, currentUser.ID).Scan(&count)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if count != 1 {
			http.Error(w, "The post could not be found.", http.StatusBadRequest)
			return
		}
	}
	if len(post_type) == 0 {
		post_type = "0"
	} else if post_type == "1" {
		if len(painting) == 0 {
			http.Error(w, "You must add a drawing.", http.StatusBadRequest)
			return
		}
	} else if post_type == "2" {
		if len(r.FormValue("option-a")) == 0 || len(r.FormValue("option-b")) == 0 {
			http.Error(w, "Polls must have at least two options.", http.StatusBadRequest)
			return
		}
	} else if post_type != "0" {
		http.Error(w, "Invalid post type.", http.StatusBadRequest)
		return
	}

	if len(body) > 0 {
		matched := youtube.FindAllStringSubmatch(body, 1)
		if len(matched) > 0 {
			url = matched[0][1]
			url_type = 1
		} else {
			matched = spotify.FindAllStringSubmatch(body, 1)
			if len(matched) > 0 {
				url = matched[0][1]
				url_type = 2
			} else {
				matched = soundcloud.FindAllStringSubmatch(body, 1)
				if len(matched) > 0 {
					url = "https://" + matched[0][0]
					url_type = 3
				}
			}
		}
	}

	stmt, err := db.Prepare("INSERT posts SET created_by = ?, community_id = ?, body = ?, image = ?, attachment_type = ?, url = ?, url_type = ?, is_spoiler = ?, feeling = ?, privacy = ?, repost = ?, post_type = ?, migrated_id = '', migrated_community = 0")
	if err == nil {
		// If there's no errors, we can go ahead and execute the statement.
		_, err = stmt.Exec(&user_id, &community_id, &body, &image, &attachment_type, &url, &url_type, &is_spoiler, &feeling, &privacy, &repost, &post_type)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var posts = post{}
		var timestamp time.Time
		var role int

		err = db.QueryRow("SELECT posts.id, created_by, created_at, feeling, body, image, attachment_type, is_spoiler, post_type, url, url_type, pinned, privacy, repost, communities.id, title, icon, username, nickname, avatar, has_mh, hide_online, color, role FROM posts LEFT JOIN communities ON communities.id = community_id LEFT JOIN users ON users.id = created_by WHERE created_by = ? ORDER BY created_at DESC LIMIT 1", user_id).Scan(&posts.ID, &posts.CreatedBy, &timestamp, &posts.Feeling, &posts.BodyText, &posts.Image, &posts.AttachmentType, &posts.IsSpoiler, &posts.PostType, &posts.URL, &posts.URLType, &posts.Pinned, &posts.Privacy, &posts.RepostID, &posts.CommunityID, &posts.CommunityName, &posts.CommunityIcon, &posts.PosterUsername, &posts.PosterNickname, &posts.PosterIcon, &posts.PosterHasMii, &posts.PosterHideOnline, &posts.PosterColor, &role)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if posts.PostType == 2 {
			_, err = db.Exec("INSERT INTO options (post, name) VALUES (?, ?), (?, ?)", posts.ID, r.FormValue("option-a"), posts.ID, r.FormValue("option-b"))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if len(r.FormValue("option-c")) > 0 {
				_, err = db.Exec("INSERT INTO options (post, name) VALUES (?, ?)", posts.ID, r.FormValue("option-c"))
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
			if len(r.FormValue("option-d")) > 0 {
				_, err = db.Exec("INSERT INTO options (post, name) VALUES (?, ?)", posts.ID, r.FormValue("option-d"))
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
			if len(r.FormValue("option-e")) > 0 {
				_, err = db.Exec("INSERT INTO options (post, name) VALUES (?, ?)", posts.ID, r.FormValue("option-e"))
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
			posts.Poll = getPoll(posts.ID, currentUser.ID)
		}

		posts.PosterIcon = getAvatar(posts.PosterIcon, posts.PosterHasMii, posts.Feeling)
		if role > 0 {
			posts.PosterRoleImage = getRoleImage(role)
		}
		posts.CreatedAt = humanTiming(timestamp, currentUser.Timezone)
		posts.CreatedAtUnix = timestamp.Unix()
		posts.Body = parseBodyWithLineBreaks(posts.BodyText, true, true)
		posts.ByMe = true
		posts.CanYeah = true // temporary!
		if posts.RepostID > 0 {
			var repost post
			db.QueryRow("SELECT posts.id, created_by, created_at, edited_at, feeling, body, image, attachment_type, is_spoiler, post_type, url, url_type, pinned, privacy, repost, migration, migrated_id, migrated_community, is_rm_by_admin, communities.id, title, icon, rm, username, nickname, avatar, has_mh, online, hide_online, color, role FROM posts LEFT JOIN communities ON communities.id = community_id LEFT JOIN users ON users.id = created_by WHERE posts.id = ? AND is_rm = 0 AND is_rm_by_admin = 0 AND users.id NOT IN (SELECT if(source = ?, target, source) FROM blocks WHERE (source = ? AND target = users.id) OR (source = users.id AND target = ?)) AND IF(created_by = ?, true, LOWER(body) NOT REGEXP LOWER(?)) AND (privacy = 0 OR (privacy IN (1, 2, 3, 4) AND (SELECT COUNT(*) FROM friendships WHERE source = ? AND target = created_by OR source = created_by AND target = ? LIMIT 1) = 1) OR (privacy IN (1, 3, 5, 6) AND (SELECT COUNT(*) FROM follows WHERE follow_to = created_by AND follow_by = ? LIMIT 1) = 1) OR (privacy IN (1, 2, 5, 7) AND (SELECT COUNT(*) FROM follows WHERE follow_to = ? AND follow_by = created_by) = 1) OR (privacy = 8 AND ? > 0) OR created_by = ?) LIMIT 1", posts.RepostID, currentUser.ID, currentUser.ID, currentUser.ID, currentUser.ID, escapeForbiddenKeywords(currentUser.ForbiddenKeywords), currentUser.ID, currentUser.ID, currentUser.ID, currentUser.ID, currentUser.Level, currentUser.ID).Scan(&repost.ID, &repost.CreatedBy, &repost.CreatedAtTime, &repost.EditedAtTime, &repost.Feeling, &repost.BodyText, &repost.Image, &repost.AttachmentType, &repost.IsSpoiler, &repost.PostType, &repost.URL, &repost.URLType, &repost.Pinned, &repost.Privacy, &repost.RepostID, &repost.MigrationID, &repost.MigratedID, &repost.MigratedCommunity, &repost.IsRMByAdmin, &repost.CommunityID, &repost.CommunityName, &repost.CommunityIcon, &repost.CommunityRM, &repost.PosterUsername, &repost.PosterNickname, &repost.PosterIcon, &repost.PosterHasMii, &repost.PosterOnline, &repost.PosterHideOnline, &repost.PosterColor, &repost.PosterRoleID)
			posts.Repost = &repost
			posts.Repost.Type = 3
			if len(posts.Repost.CommunityName) > 0 {
				posts.Repost = setupPost(posts.Repost, currentUser, 3, 0)
			}
		}

		err = templates.ExecuteTemplate(w, "render_post.html", posts)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

		var CommunityPostTpl, UserPostTpl bytes.Buffer
		posts.ByMe = false
		templates.ExecuteTemplate(&CommunityPostTpl, "render_post.html", posts)
		posts.Type = 1
		templates.ExecuteTemplate(&UserPostTpl, "render_post.html", posts)

		if repost != "0" && posts.Repost.CreatedBy != currentUser.ID {
			createNotif(posts.Repost.CreatedBy, 7, strconv.Itoa(posts.ID), currentUser.ID)
		}

		var msg wsMessage
		msg.Type = "post"
		msg.Content = CommunityPostTpl.String()

		for client := range clients {
			//clients[client].Mutex.Lock()
			if clients[client].OnPage == "/communities/"+community_id && clients[client].UserID != posts.CreatedBy && (!checkIfEitherBlocked(clients[client].UserID, posts.CreatedBy) || clients[client].Level > 0) && !inForbiddenKeywords(body, clients[client].UserID) && (posts.Privacy == 0) {
				msg.Content = CommunityPostTpl.String()
				err := writeWs(clients[client], client, msg)
				if err != nil {
					client.Close()
					delete(clients, client)
				}
			} else if clients[client].OnPage == "/users/"+currentUser.Username+"/posts" && (!checkIfEitherBlocked(clients[client].UserID, posts.CreatedBy) || clients[client].Level > 0) && !inForbiddenKeywords(body, clients[client].UserID) && (posts.Privacy == 0) {
				msg.Content = UserPostTpl.String()
				err := writeWs(clients[client], client, msg)
				if err != nil {
					client.Close()
					delete(clients, client)
				}
			}
			//clients[client].Mutex.Unlock()
		}
	} else {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Give a Yeah to a post.
func createPostYeah(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	vars := mux.Vars(r)

	post_id := vars["id"]
	user_id := currentUser.ID

	var post_by int
	var community_id string
	var feeling int
	var yeah_exists int

	// Check if the post exists; if it doesn't, the Yeah wont be added.
	db.QueryRow("SELECT created_by, community_id, feeling FROM posts WHERE id = ?", post_id).Scan(&post_by, &community_id, &feeling)

	if post_by != 0 {
		// Check if the post has already been yeahed.
		db.QueryRow("SELECT id FROM yeahs WHERE yeah_post = ? AND yeah_by = ? AND on_comment = 0", post_id, user_id).Scan(&yeah_exists)
		if yeah_exists != 0 {
			return
		}

		if checkIfCanYeah(currentUser, post_by) {
			stmt, err := db.Prepare("INSERT yeahs SET yeah_post = ?, yeah_by = ?, on_comment = 0")
			if err == nil {
				// If there's no errors, we can go ahead and execute the statement.
				_, err := stmt.Exec(&post_id, &user_id)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				} else {
					createNotif(post_by, 0, post_id, user_id)

					var msg wsMessage
					var yeahs = yeah{}

					db.QueryRow("SELECT yeahs.id, username, avatar, has_mh FROM yeahs LEFT JOIN users ON users.id = yeah_by WHERE yeah_by = ? ORDER BY yeahs.id DESC LIMIT 1", user_id).Scan(&yeahs.ID, &yeahs.Username, &yeahs.Avatar, &yeahs.HasMii)

					msg.Type = "postYeah"
					msg.ID = post_id
					mariosPrincessSex := "\">"
					if len(currentUser.Role.Image) > 0 {
						mariosPrincessSex = fmt.Sprintf(" official-user\"><img src=\"%s\" class=\"official-tag\">", currentUser.Role.Image)
					}
					// "I don't think we need a separate template for such a small amount of HTML." -whoever originally wrote this function
					msg.Content = fmt.Sprintf("<a href=\"/users/%s\" id=\"%d\" class=\"post-permalink-feeling-icon%s<img src=\"%s\" class=\"user-icon\"></a>", yeahs.Username, yeahs.ID, html.EscapeString(mariosPrincessSex), getAvatar(yeahs.Avatar, yeahs.HasMii, feeling))

					for client := range clients {
						//clients[client].Mutex.Lock()
						if (clients[client].OnPage == "/communities/"+community_id || clients[client].OnPage == "/posts/"+post_id) && clients[client].UserID != user_id {
							err := writeWs(clients[client], client, msg)
							if err != nil {
								client.Close()
								delete(clients, client)
							}
						}
						//clients[client].Mutex.Unlock()
					}
				}
			}
		}
	}
}

// Delete comments.
func deleteComment(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	vars := mux.Vars(r)
	comment_id := vars["id"]

	created_by := -1
	sex := -1
	db.QueryRow("SELECT created_by, post FROM comments WHERE id = ?", comment_id).Scan(&created_by, &sex)
	if created_by == -1 || sex == -1 {
		handle404(w, r)
		return
	}

	if created_by != currentUser.ID {
		var otherUserLevel int
		db.QueryRow("SELECT level FROM users WHERE id = ?", created_by).Scan(&otherUserLevel)
		if otherUserLevel > currentUser.Level || currentUser.Level == 0 {
			http.Error(w, "You do not have permission to delete this comment.", http.StatusForbidden)
			return
		}
		_, err = db.Exec("UPDATE comments SET is_rm_by_admin = 1 WHERE id = ?", comment_id)
	} else {
		_, err = db.Exec("UPDATE comments SET is_rm = 1 WHERE id = ?", comment_id)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	db.Exec("DELETE FROM reports WHERE pid = ? AND type = 1", comment_id)

	var msg wsMessage
	msg.ID = comment_id
	for client := range clients {
		//clients[client].Mutex.Lock()
		if clients[client].OnPage == "/posts/"+strconv.Itoa(sex) {
			msg.Type = "delete"
		} else if clients[client].OnPage == "/comments/"+comment_id && clients[client].UserID != currentUser.ID {
			msg.Type = "refresh"
		} else {
			continue
		}
		err := writeWs(clients[client], client, msg)
		if err != nil {
			client.Close()
			delete(clients, client)
		}
		//clients[client].Mutex.Unlock()
	}
}

// Unyeah a comment.
func deleteCommentYeah(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	vars := mux.Vars(r)

	var yeah_id string
	var post_id string
	comment_id := vars["id"]
	user_id := currentUser.ID

	db.QueryRow("SELECT yeahs.id, comments.post FROM yeahs INNER JOIN comments ON comments.id = yeahs.yeah_post WHERE yeah_post = ? AND yeah_by = ? AND on_comment = 1", comment_id, user_id).Scan(&yeah_id, &post_id)

	if yeah_id != "" {
		stmt, _ := db.Prepare("DELETE FROM yeahs WHERE yeah_post = ? AND yeah_by = ? AND on_comment = 1")
		stmt.Exec(&comment_id, &user_id)

		var msg wsMessage
		msg.Type = "commentUnyeah"
		msg.ID = comment_id
		msg.Content = yeah_id

		for client := range clients {
			//clients[client].Mutex.Lock()
			if (clients[client].OnPage == "/posts/"+post_id || clients[client].OnPage == "/comments/"+comment_id) && clients[client].UserID != user_id {
				err := writeWs(clients[client], client, msg)
				if err != nil {
					client.Close()
					delete(clients, client)
				}
			}
			//clients[client].Mutex.Unlock()
		}
	}
}

// Remove a favorite from a community.
func deleteCommunityFavorite(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	vars := mux.Vars(r)
	community_id := vars["id"]

	var communityExists int
	err := db.QueryRow("SELECT COUNT(*) FROM communities WHERE id = ?", community_id).Scan(&communityExists)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if communityExists == 0 {
		handle404(w, r)
		return
	}

	stmt, err := db.Prepare("DELETE FROM community_favorites WHERE community = ? AND favorite_by = ?")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	stmt.Exec(&community_id, &currentUser.ID)
}

// Unfollow a user.
func deleteFollow(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	vars := mux.Vars(r)

	username := vars["username"]
	var user_id int
	var usern string
	db.QueryRow("SELECT id, username FROM users WHERE username = ?", username).Scan(&user_id, &usern)
	if len(usern) == 0 {
		handle404(w, r)
		return
	}

	stmt, _ := db.Prepare("DELETE FROM follows WHERE follow_to = ? AND follow_by = ?")
	stmt.Exec(&user_id, &currentUser.ID)

	var msg wsMessage
	msg.Type = "unfollow"

	for client := range clients {
		//clients[client].Mutex.Lock()
		if strings.HasPrefix(clients[client].OnPage, "/users/"+username) {
			err := writeWs(clients[client], client, msg)
			if err != nil {
				client.Close()
				delete(clients, client)
			}
		}
		//clients[client].Mutex.Unlock()
	}
}

// Delete a friend.
func deleteFriend(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	vars := mux.Vars(r)
	username := vars["username"]
	var user_id int

	err := db.QueryRow("SELECT id FROM users WHERE username = ?", username).Scan(&user_id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if user_id == 0 {
		http.Error(w, "That user does not exist.", http.StatusBadRequest)
		return
	}

	stmt, err := db.Prepare("DELETE FROM friendships WHERE (source = ? AND target = ?) OR (source = ? AND target = ?)")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	stmt.Exec(&user_id, &currentUser.ID, &currentUser.ID, &user_id)

	stmt, err = db.Prepare("UPDATE conversations SET is_rm = 1 WHERE (source = ? AND target = ?) OR (source = ? AND target = ?)")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	stmt.Exec(&user_id, &currentUser.ID, &currentUser.ID, &user_id)
}

// Delete a group chat.
func deleteGroupChat(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	// No need to validate any of this since you can't fake a currentUser.
	vars := mux.Vars(r)
	conversationID := vars["id"]
	stmt, err := db.Prepare("DELETE FROM conversations WHERE id = ? AND source = ? AND target = 0")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	stmt.Exec(&conversationID, currentUser.ID)

	http.Redirect(w, r, "/messages", 301)
}

// Delete a message.
func deleteMessage(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	vars := mux.Vars(r)
	message_id := vars["id"]

	var sex int
	err = db.QueryRow("SELECT COUNT(*) FROM messages WHERE id = ? AND created_by = ?", message_id, currentUser.ID).Scan(&sex)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if sex == 0 {
		http.Error(w, "You can only delete messages you've created.", http.StatusBadRequest)
		return
	}

	stmt, err := db.Prepare("UPDATE messages SET is_rm = 1 WHERE id = ?")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, err = stmt.Exec(&message_id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Get the conversation ID and the username of the user on the other end (if it's not a group chat) for websockets.
	var conversationID string
	var otherUserID int
	err = db.QueryRow("SELECT conversations.id, IF(source = ?, source, target) FROM conversations LEFT JOIN messages ON conversation_id = conversations.id WHERE messages.id = ?", currentUser.ID, message_id).Scan(&conversationID, &otherUserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var msg wsMessage
	msg.ID = message_id
	for client := range clients {
		//clients[client].Mutex.Lock()
		fmt.Println("otherUserID: " + strconv.Itoa(otherUserID))
		fmt.Println("conversationID: " + conversationID)
		fmt.Println("onpage: " + clients[client].OnPage)
		fmt.Println(clients[client].OnPage)
		if otherUserID > 0 {
			if clients[client].OnPage == "/messages/"+url.PathEscape(currentUser.Username) && clients[client].UserID == otherUserID {
				msg.Type = "delete"
			} else {
				continue
			}
		} else {
			if clients[client].OnPage == "/conversations/"+conversationID {
				db.QueryRow("SELECT COUNT(*) FROM group_members WHERE user = ? AND conversation = ?", clients[client].UserID, conversationID).Scan(&otherUserID)
				if otherUserID == 0 {
					continue
				}
				msg.Type = "delete"
			} else {
				continue
			}
		}
		err := writeWs(clients[client], client, msg)
		if err != nil {
			client.Close()
			delete(clients, client)
		}
		//clients[client].Mutex.Unlock()
	}
}

// Delete a post.
func deletePost(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	vars := mux.Vars(r)
	post_id := vars["id"]

	var created_by int
	community_id := -1
	db.QueryRow("SELECT created_by, community_id FROM posts WHERE id = ?", post_id).Scan(&created_by, &community_id)
	if community_id == -1 {
		handle404(w, r)
		return
	}
	if created_by != currentUser.ID {
		var otherUserLevel int
		db.QueryRow("SELECT level FROM users WHERE id = ?", created_by).Scan(&otherUserLevel)
		if otherUserLevel > currentUser.Level || currentUser.Level == 0 {
			http.Error(w, "You do not have permission to delete this post.", http.StatusForbidden)
			return
		}
		_, err = db.Exec("UPDATE posts SET is_rm_by_admin = 1 WHERE id = ?", post_id)
	} else {
		_, err = db.Exec("UPDATE posts SET is_rm = 1 WHERE id = ?", post_id)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if created_by != currentUser.ID {
		reason := -1
		db.QueryRow("SELECT reason FROM reports WHERE pid = ? AND type = 0 ORDER BY COUNT(reason) DESC LIMIT 1", post_id).Scan(&reason)
		if reason != -1 {
			_, err = db.Exec("INSERT INTO admin_notifications (reason, post, type, user) VALUES (?, ?, 0, ?)", reason, post_id, created_by)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			_, err = db.Exec("REPLACE INTO notifications (notif_type, notif_to) VALUES (7, ?)", created_by)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}
	db.Exec("DELETE FROM reports WHERE pid = ? AND type = 0", post_id)

	var msg wsMessage
	msg.ID = post_id
	for client := range clients {
		//clients[client].Mutex.Lock()
		if clients[client].OnPage == "/communities/"+strconv.Itoa(community_id) {
			msg.Type = "delete"
		} else if clients[client].OnPage == "/posts/"+post_id && clients[client].UserID != currentUser.ID {
			msg.Type = "refresh"
		} else {
			continue
		}
		err := writeWs(clients[client], client, msg)
		if err != nil {
			client.Close()
			delete(clients, client)
		}
		//clients[client].Mutex.Unlock()
	}
}

// Unyeah a post.
func deletePostYeah(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	vars := mux.Vars(r)

	var yeah_id string
	var community_id string
	post_id := vars["id"]
	user_id := currentUser.ID

	db.QueryRow("SELECT yeahs.id, posts.community_id FROM yeahs INNER JOIN posts ON posts.id = yeahs.yeah_post WHERE yeah_post = ? AND yeah_by = ? AND on_comment = 0", post_id, user_id).Scan(&yeah_id, &community_id)

	if yeah_id != "" {
		stmt, _ := db.Prepare("DELETE FROM yeahs WHERE yeah_post = ? AND yeah_by = ? AND on_comment = 0")
		stmt.Exec(&post_id, &user_id)

		var msg wsMessage
		msg.Type = "postUnyeah"
		msg.ID = post_id
		msg.Content = yeah_id

		for client := range clients {
			//clients[client].Mutex.Lock()
			if (clients[client].OnPage == "/communities/"+community_id || clients[client].OnPage == "/posts/"+post_id) && clients[client].UserID != user_id {
				err := writeWs(clients[client], client, msg)
				if err != nil {
					client.Close()
					delete(clients, client)
				}
			}
			//clients[client].Mutex.Unlock()
		}
	}
}

// Change a user's account settings.
func editAccountSettings(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	yeah_notifications := r.FormValue("yeah_notifications")
	hide_online := r.FormValue("hide_online")
	hide_last_seen := r.FormValue("hide_last_seen")
	group_permissions := r.FormValue("group_permissions")
	websockets_enabled := r.FormValue("websockets_enabled")
	if len(yeah_notifications) == 0 {
		yeah_notifications = "0"
	}
	if len(hide_online) == 0 {
		hide_online = "0"
	}
	if len(hide_last_seen) == 0 {
		hide_last_seen = "0"
	}
	if len(group_permissions) == 0 {
		group_permissions = "0"
	}
	if len(websockets_enabled) == 0 {
		websockets_enabled = "0"
	}

	stmt, err := db.Prepare("UPDATE users SET yeah_notifications = ?, hide_online = ?, hide_last_seen = ?, group_permissions = ?, websockets_enabled = ? WHERE id = ?")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	stmt.Exec(&yeah_notifications, &hide_online, &hide_last_seen, &group_permissions, &websockets_enabled, &currentUser.ID)
}

// Edit comments.
func editComment(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	vars := mux.Vars(r)
	comment_id := vars["id"]

	var sex int
	err := db.QueryRow("SELECT COUNT(*) FROM comments WHERE id = ? AND created_by = ?", comment_id, currentUser.ID).Scan(&sex)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if sex == 0 {
		handle404(w, r)
		return
	}

	body := r.FormValue("body")
	is_spoiler := r.FormValue("is_spoiler")
	feeling := r.FormValue("feeling_id")
	if utf8.RuneCountInString(body) > 2000 {
		http.Error(w, "Your comment is too long. (2000 characters maximum)", http.StatusBadRequest)
		return
	}
	if len(body) == 0 { // todo: add code to make this work with blank image comments
		http.Error(w, "Your comment is empty.", http.StatusBadRequest)
		return
	}
	if len(is_spoiler) == 0 {
		is_spoiler = "0"
	}

	stmt, err := db.Prepare("UPDATE comments SET edited_at = now(), body = ?, is_spoiler = ?, feeling = ? WHERE id = ?")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, err = stmt.Exec(&body, &is_spoiler, &feeling, &comment_id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var msg wsMessage
	msg.ID = comment_id
	msg.Type = "commentEdit"
	msg.Content = string(parseBody(body, false, true))
	for client := range clients {
		var post_id string
		db.QueryRow("SELECT post FROM comments WHERE id = ?", comment_id).Scan(&post_id)
		//clients[client].Mutex.Lock()
		if clients[client].OnPage == "/posts/"+post_id || clients[client].OnPage == "/comments/"+comment_id {
			err := writeWs(clients[client], client, msg)
			if err != nil {
				client.Close()
				delete(clients, client)
			}
		}
		//clients[client].Mutex.Unlock()
	}
}

// Edit a group chat.
func editGroupChat(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	var users []int
	for i := 1; i <= 10; i++ {
		username := r.FormValue("user" + strconv.Itoa(i))
		if len(username) > 0 {
			var id int
			var group_permissions int
			db.QueryRow("SELECT id, group_permissions FROM users WHERE username = ?", username).Scan(&id, &group_permissions)
			if id == 0 {
				http.Error(w, "The user "+username+" does not exist.", http.StatusBadRequest)
				return
			}
			if group_permissions == 1 {
				var followCount int
				db.QueryRow("SELECT COUNT(*) FROM follows WHERE follow_to = ? AND follow_by = ?", currentUser.ID, id).Scan(&followCount)
				if followCount == 0 {
					http.Error(w, "The user "+username+" does not allow you to add them to chat groups.", http.StatusBadRequest)
					return
				}
			}
			var friendCount int
			db.QueryRow("SELECT COUNT(*) FROM friendships WHERE (source = ? AND target = ?) OR (source = ? AND target = ?)", id, currentUser.ID, currentUser.ID, id).Scan(&friendCount)
			if friendCount == 0 {
				http.Error(w, "The user "+username+" is not on your friend list.", http.StatusBadRequest)
				return
			}

			users = append(users, id)
		}
	}
	users = append(users, currentUser.ID)

	vars := mux.Vars(r)
	conversationID := vars["id"]
	stmt, err := db.Prepare("DELETE FROM group_members WHERE conversation = ?")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	stmt.Exec(&conversationID)

	for _, user := range users {
		stmt, err = db.Prepare("INSERT INTO group_members (user, conversation) VALUES (?, ?)")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		stmt.Exec(&user, &conversationID)
	}

	http.Redirect(w, r, "/conversations/"+conversationID, 301)
}

// Edit a post.
func editPost(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	vars := mux.Vars(r)
	post_id := vars["id"]

	var sex int
	err := db.QueryRow("SELECT COUNT(*) FROM posts WHERE id = ? AND created_by = ?", post_id, currentUser.ID).Scan(&sex)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if sex == 0 {
		handle404(w, r)
		return
	}

	body := r.FormValue("body")
	is_spoiler := r.FormValue("is_spoiler")
	feeling := r.FormValue("feeling_id")
	privacy := r.FormValue("privacy")
	if utf8.RuneCountInString(body) > 2000 {
		http.Error(w, "Your post is too long. (2000 characters maximum)", http.StatusBadRequest)
		return
	}
	if len(body) == 0 { // todo: add code to make this work with blank image posts
		http.Error(w, "Your post is empty.", http.StatusBadRequest)
		return
	}
	if len(is_spoiler) == 0 {
		is_spoiler = "0"
	}
	if len(feeling) == 0 {
		feeling = "0"
	}
	if len(privacy) != 1 {
		privacy = "0"
	}

	stmt, err := db.Prepare("UPDATE posts SET edited_at = now(), body = ?, is_spoiler = ?, feeling = ?, privacy = ? WHERE id = ?")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, err = stmt.Exec(&body, &is_spoiler, &feeling, &privacy, &post_id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var msg wsMessage
	msg.ID = post_id
	msg.Type = "postEdit"
	for client := range clients {
		var community_id string
		db.QueryRow("SELECT community_id FROM posts WHERE id = ?", post_id).Scan(&community_id)
		//clients[client].Mutex.Lock()
		if clients[client].OnPage == "/posts/"+post_id {
			msg.Content = string(parseBody(body, false, true))
		} else if clients[client].OnPage == "/communities/"+community_id {
			msg.Content = string(parseBodyWithLineBreaks(body, false, true))
		} else {
			continue
		}
		err := writeWs(clients[client], client, msg)
		if err != nil {
			client.Close()
			delete(clients, client)
		}
		//clients[client].Mutex.Unlock()
	}
}

// Change a user's profile settings.
func editProfileSettings(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	var newUser user
	var newProfile profile
	newUser.Nickname = r.FormValue("screen_name")
	newProfile.CommentText = r.FormValue("profile_comment")
	newProfile.Region = r.FormValue("country")
	newProfile.Discord = r.FormValue("discord")
	newProfile.YouTube = r.FormValue("website")
	newProfile.PSN = r.FormValue("psn")
	newProfile.SwitchCode = r.FormValue("switch_code")
	newProfile.Twitter = r.FormValue("external")
	newProfile.Steam = r.FormValue("steam")
	newUser.Color = r.FormValue("color")
	newUser.Theme = r.FormValue("theme")
	newProfile.NNIDVisibility, _ = strconv.Atoi(r.FormValue("id_visibility"))
	newProfile.AllowFriend, _ = strconv.Atoi(r.FormValue("let_friendrequest"))
	genderNumber, _ := strconv.Atoi(r.FormValue("pronoun_dot_is"))
	newProfile.YeahVisibility, _ = strconv.Atoi(r.FormValue("yeahs_visibility"))
	newProfile.ReplyVisibility, _ = strconv.Atoi(r.FormValue("comments_visibility"))
	newUser.DefaultPrivacy, _ = strconv.Atoi(r.FormValue("default_privacy"))
	newUser.ForbiddenKeywords = r.FormValue("forbidden_keywords")
	newUser.Email = r.FormValue("email")
	newProfile.NNID = r.FormValue("nnid")
	newProfile.MiiHash = r.FormValue("mh")
	newProfile.AvatarID, _ = strconv.Atoi(r.FormValue("image"))
	newUser.HasMii, _ = strconv.ParseBool(r.FormValue("avatar"))

	if utf8.RuneCountInString(newUser.Nickname) > 32 {
		http.Error(w, "Your nickname is too long.", http.StatusBadRequest)
		return
	}
	if len(newUser.Nickname) == 0 || newUser.Nickname == " " {
		http.Error(w, "You must specify a nickname.", http.StatusBadRequest)
		return
	}
	if utf8.RuneCountInString(newProfile.CommentText) > 2000 {
		http.Error(w, "Your profile comment is too long.", http.StatusBadRequest)
		return
	}
	if utf8.RuneCountInString(newProfile.Region) > 64 {
		http.Error(w, "Your region is too long.", http.StatusBadRequest)
		return
	}
	if utf8.RuneCountInString(newProfile.Discord) > 37 {
		http.Error(w, "Your DiscordTag is too long.", http.StatusBadRequest)
		return
	}
	if utf8.RuneCountInString(newProfile.YouTube) > 1024 {
		http.Error(w, "Your URL is too long.", http.StatusBadRequest)
		return
	}
	if len(newProfile.YouTube) > 0 {
		uri, err := url.Parse(newProfile.YouTube)
		if err != nil {
			http.Error(w, "Your URL is invalid.", http.StatusBadRequest)
			return
		}
		if uri.Scheme == "" || uri.Host == "" {
			http.Error(w, "Your URL is invalid.", http.StatusBadRequest)
			return
		}
	}
	if utf8.RuneCountInString(newProfile.PSN) > 16 {
		http.Error(w, "Your PSN is too long.", http.StatusBadRequest)
		return
	}
	if utf8.RuneCountInString(newProfile.SwitchCode) > 17 {
		http.Error(w, "Your Nintendo Switch friend code is too long.", http.StatusBadRequest)
		return
	}
	if utf8.RuneCountInString(newProfile.Twitter) > 15 {
		http.Error(w, "Your Twitter username is too long.", http.StatusBadRequest)
		return
	}
	if utf8.RuneCountInString(newProfile.Steam) > 64 {
		http.Error(w, "Your Steam username is too long.", http.StatusBadRequest)
		return
	}
	if newUser.Color == "#000000" {
		newUser.Color = ""
	}
	if len(newUser.Color) != 7 && len(newUser.Color) != 0 {
		http.Error(w, "Your nickname color is invalid.", http.StatusBadRequest)
		return
	}
	if len(newUser.Color) > 0 {
		_, err = colorful.Hex(newUser.Color)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if newUser.Theme == "#8000ff" {
		newUser.Theme = ""
	}
	if len(newUser.Theme) != 7 && len(newUser.Theme) != 0 {
		http.Error(w, "Your theme color is invalid.", http.StatusBadRequest)
		return
	}
	if len(newUser.Theme) > 0 {
		themeColor, err := colorful.Hex(newUser.Theme)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		h, s, v := themeColor.Hsv()
		s = s * 0.1
		newUser.Theme = newUser.Theme + "," + colorful.Hsv(h, s, v).Hex()
		_, s, _ = themeColor.Hsv()
		v = v * 0.25
		newUser.Theme = newUser.Theme + "," + colorful.Hsv(h, s, v).Hex()
		_, _, v = themeColor.Hsv()
		v = v * 0.125
		newUser.Theme = newUser.Theme + "," + colorful.Hsv(h, s, v).Hex()
	}
	if newProfile.NNIDVisibility > 2 || newProfile.NNIDVisibility < 0 {
		http.Error(w, "Invalid NNID visibility value.", http.StatusBadRequest)
		return
	}
	if genderNumber > 5 || genderNumber < 0 {
		http.Error(w, "There are only five genders.", http.StatusBadRequest)
		return
	}
	if newProfile.AllowFriend > 2 || newProfile.AllowFriend < 0 {
		http.Error(w, "Invalid friend request allowance value.", http.StatusBadRequest)
		return
	}
	if newProfile.YeahVisibility > 2 || newProfile.YeahVisibility < 0 {
		http.Error(w, "Invalid yeah visibility value.", http.StatusBadRequest)
		return
	}
	if newProfile.ReplyVisibility > 2 || newProfile.ReplyVisibility < 0 {
		http.Error(w, "Invalid reply visibility value.", http.StatusBadRequest)
		return
	}
	if newUser.DefaultPrivacy > 9 {
		http.Error(w, "Invalid default privacy value.", http.StatusBadRequest)
		return
	}
	if len(newUser.ForbiddenKeywords) > 2000 {
		http.Error(w, "Your set of forbidden keywords is too long.", http.StatusBadRequest)
		return
	}
	if len(newUser.Email) > 255 {
		http.Error(w, "Your email address is too long.", http.StatusBadRequest)
		return
	}
	if len(newProfile.NNID) > 0 {
		nnidCheck, _ := regexp.MatchString("^[A-Za-z0-9-._]{6,16}$", newProfile.NNID)
		if !nnidCheck {
			http.Error(w, "Your Nintendo Network ID is invalid.", http.StatusBadRequest)
			return
		}
	}

	if newProfile.AvatarID > 0 {
		imageURL := ""
		db.QueryRow("SELECT value FROM images WHERE id = ?", newProfile.AvatarID).Scan(&imageURL)
		if len(imageURL) == 0 {
			http.Error(w, "Invalid image.", http.StatusBadRequest)
			return
		}
		newProfile.AvatarImage = imageURL
	}
	if newUser.HasMii {
		newUser.Avatar = newProfile.MiiHash
	} else {
		newUser.Avatar = newProfile.AvatarImage
	}

	stmt, err := db.Prepare("UPDATE users SET nickname = ?, color = ?, theme = ?, forbidden_keywords = ?, default_privacy = ?, has_mh = ?, avatar = ?, email = ? WHERE id = ?")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, err = stmt.Exec(newUser.Nickname, newUser.Color, newUser.Theme, newUser.ForbiddenKeywords, newUser.DefaultPrivacy, newUser.HasMii, newUser.Avatar, newUser.Email, currentUser.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	stmt, err = db.Prepare("UPDATE profiles SET comment = ?, region = ?, discord = ?, youtube = ?, psn = ?, switch_code = ?, twitter = ?, steam = ?, nnid_visibility = ?, allow_friend = ?, gender = ?, yeah_visibility = ?, reply_visibility = ?, nnid = ?, mh = ?, avatar_image = ?, avatar_id = ? WHERE user = ?")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, err = stmt.Exec(newProfile.CommentText, newProfile.Region, newProfile.Discord, newProfile.YouTube, newProfile.PSN, newProfile.SwitchCode, newProfile.Twitter, newProfile.Steam, newProfile.NNIDVisibility, newProfile.AllowFriend, genderNumber, newProfile.YeahVisibility, newProfile.ReplyVisibility, newProfile.NNID, newProfile.MiiHash, newProfile.AvatarImage, newProfile.AvatarID, currentUser.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// Favorite a post.
func favoritePost(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	vars := mux.Vars(r)
	post_id := vars["id"]

	var sex int
	err := db.QueryRow("SELECT COUNT(*) FROM posts WHERE id = ? AND is_rm = 0 AND is_rm_by_admin = 0", post_id).Scan(&sex)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if sex == 0 {
		handle404(w, r)
		return
	}

	stmt, err := db.Prepare("UPDATE profiles SET favorite = ? WHERE user = ?")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, err = stmt.Exec(&post_id, currentUser.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// Get a Mii from a Nintendo Network ID.
func getMii(w http.ResponseWriter, r *http.Request) {
	nnid := r.FormValue("a")
	nnidCheck, _ := regexp.MatchString("^[A-Za-z0-9-._]{6,16}$", nnid)
	if !nnidCheck {
		http.Error(w, "Your Nintendo Network ID is invalid.", http.StatusBadRequest)
		return
	}
	resp, err := http.Get("https://ariankordi.net/seth/" + nnid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if resp.StatusCode == 404 {
		http.Error(w, "The Nintendo Network ID you provided doesn't exist.", http.StatusNotFound)
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	w.Write(body)
}

// Get notification counts.
func getNotificationCounts(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
	}
	w.Header().Add("Content-Type", "application/json")

	//if(!currentUser.WebsocketsEnabled) {
	//wait, _ := time.ParseDuration("50s")
	//time.AfterFunc(wait, )
	//}

	checkUpdate, err := json.Marshal(map[string]interface{}{
		"success": true,
		"n":       currentUser.Notifications.Notifications,
		"msg":     currentUser.Notifications.Messages,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write([]byte(checkUpdate))
}

// Get a user's region.
func getRegion(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 302)
		return
	}

	if isGeoIPEnabled == false {
		http.Error(w, "GeoIP is not enabled on this instance of Indigo. Check the README file to learn how to fix this.", http.StatusInternalServerError)
		return
	}

	ip := getIP(r)
	parsedHost, _, err := net.SplitHostPort(ip)
	if err != nil {
		http.Error(w, "Your IP ("+ip+") is not a host/port combination.", http.StatusBadRequest)
	}
	parsedIP := net.ParseIP(parsedHost)
	if parsedIP == nil {
		http.Error(w, "Your IP ("+ip+") is invalid.", http.StatusBadRequest)
		return
	}
	record, err := geoip.City(parsedIP)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if len(record.City.Names["en"]) > 0 {
		w.Write([]byte(record.City.Names["en"]))
	} else if len(record.Country.Names["en"]) > 0 {
		w.Write([]byte(record.Country.Names["en"]))
	} else if len(record.Continent.Names["en"]) > 0 {
		w.Write([]byte(record.Continent.Names["en"]))
	} else {
		http.Error(w, "Your IP ("+ip+") isn't in our database. We can't even get your CONTINENT. What the hell kind of planet are you living on!?", http.StatusInternalServerError)
	}
}

// Handle 404 Not Found requests.
func handle404(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}

	w.WriteHeader(http.StatusNotFound)
	pjax := r.Header.Get("X-PJAX") == ""
	var data = map[string]interface{}{
		"Title":       "Error",
		"Pjax":        pjax,
		"CurrentUser": currentUser,
		"Error":       "The page could not be found.",
	}
	err := templates.ExecuteTemplate(w, "error.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Handle websocket connections.
func handleConnections(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	// Upgrade initial GET request to a websocket.
	ws, _ := upgrader.Upgrade(w, r, nil)

	// Make sure we close the connection when the function returns.
	defer ws.Close()

	// Register our new client.
	// Due to an issue with Go that the original writer of this function referred to as "retarded," a second variable is needed to change .Connected.
	//clients[ws].Mutex.Lock()
	client := clients[ws]
	client.Connected = true
	client.UserID = currentUser.ID
	client.Level = currentUser.Level
	clients[ws] = client
	//clients[ws].Mutex.Unlock()

	stmt, _ := db.Prepare("UPDATE users SET online = 1, last_seen = NOW() WHERE id = ?")
	stmt.Exec(&client.UserID)

	var username string
	var hideOnline bool
	db.QueryRow("SELECT username, hide_online FROM users WHERE id = ?", client.UserID).Scan(&username, &hideOnline)

	var msg wsMessage
	if !hideOnline {
		msg.Type = "online"
	} else {
		msg.Type = "ping"
	}
	msg.Content = username

	for client := range clients {
		//clients[client].Mutex.Lock()
		if hideOnline {
			if clients[client].UserID != currentUser.ID {
				continue
			}
		}
		err := writeWs(clients[client], client, msg)
		if err != nil {
			client.Close()
			delete(clients, client)
		}
		//clients[client].Mutex.Unlock()
	}

	for {
		var msg wsMessage
		// Read in a new message as JSON and map it to a Message object.
		err := ws.ReadJSON(&msg)
		if err != nil {
			delete(clients, ws)
			break
		}

		if msg.Type == "onPage" {
			client.OnPage = msg.Content
			clients[ws].Mutex.Lock()
			clients[ws] = client
			clients[ws].Mutex.Unlock()
		}
	}

	isntOnAnotherSocket := true
	for i := range clients {
		clients[i].Mutex.Lock()
		if clients[i].UserID == client.UserID {
			isntOnAnotherSocket = false
			break
		}
		clients[i].Mutex.Unlock()
	}
	if isntOnAnotherSocket {
		stmt, _ = db.Prepare("UPDATE users SET online = 0 WHERE id = ?")
		stmt.Exec(&client.UserID)
		if !hideOnline {
			msg.Type = "offline"
			msg.Content = username

			for client := range clients {
				//clients[client].Mutex.Lock()
				err := writeWs(clients[client], client, msg)
				if err != nil {
					client.Close()
					delete(clients, client)
				}
				//clients[client].Mutex.Unlock()
			}
		}
	}
}

// The handler for the front page.
func index(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}

	pjax := r.Header.Get("X-PJAX") == ""

	featured_rows, err := db.Query("SELECT id, title, icon, banner FROM communities WHERE is_featured = 1 AND rm = 0 LIMIT 4")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var featured []community

	for featured_rows.Next() {
		var row = community{}
		err = featured_rows.Scan(&row.ID, &row.Title, &row.Icon, &row.Banner)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		featured = append(featured, row)
	}
	featured_rows.Close()

	community_rows, err := db.Query("SELECT id, title, icon, banner FROM communities WHERE rm = 0 ORDER BY id DESC LIMIT 6")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var communities []community

	for community_rows.Next() {
		var row = community{}

		err = community_rows.Scan(&row.ID, &row.Title, &row.Icon, &row.Banner)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		communities = append(communities, row)
	}
	community_rows.Close()

	var data = map[string]interface{}{
		"Title":       "Communities",
		"Pjax":        pjax,
		"CurrentUser": currentUser,
		"Featured":    featured,
		"Communities": communities,
	}

	err = templates.ExecuteTemplate(w, "index.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Leave a group chat.
func leaveGroupChat(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	// No need to validate any of this since you can't fake a currentUser.
	vars := mux.Vars(r)
	conversationID := vars["id"]
	stmt, err := db.Prepare("DELETE FROM group_members WHERE conversation = ? AND user = ?")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	stmt.Exec(&conversationID, currentUser.ID)

	// Delete the conversation if nobody is in it anymore.
	var userCount int
	db.QueryRow("SELECT COUNT(*) FROM group_members WHERE conversation = ?", conversationID).Scan(&userCount)
	if userCount == 0 {
		stmt, err := db.Prepare("DELETE FROM conversations WHERE id = ?")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		stmt.Exec(&conversationID)
	}

	http.Redirect(w, r, "/messages", 301)
}

// Log a user in.
func login(w http.ResponseWriter, r *http.Request) {
	session := sessions.Start(w, r)
	if len(session.GetString("username")) != 0 && err == nil {
		http.Redirect(w, r, "/", 302)
	}

	var currentUser user
	currentUser.LightMode = getLightMode(w, r)

	if r.Method != "POST" {
		formError := r.FormValue("error")
		var data = map[string]interface{}{
			"Title":        "Log In",
			"CurrentUser":  currentUser,
			"ForceLogins":  settings.ForceLogins,
			"AllowSignups": settings.AllowSignups,
			"FormError":    formError,
			"CSRFField":    csrf.TemplateField(r),
		}
		err := templates.ExecuteTemplate(w, "login.html", data)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	if len(settings.IPHubKey) > 0 {
		client := &http.Client{}
		ipHost, _, _ := net.SplitHostPort(getIP(r))
		req, _ := http.NewRequest("GET", "https://v2.api.iphub.info/ip/"+ipHost, nil)
		req.Header.Set("X-Key", settings.IPHubKey)
		res, _ := client.Do(req)
		defer res.Body.Close()
		body, err := ioutil.ReadAll(res.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonBody := make(map[string]interface{})
		json.Unmarshal(body, &jsonBody)
		if jsonBody["block"] == 1 {
			http.Error(w, "You cannot log in using a proxy.", http.StatusBadRequest)
			return
		}
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	users := QueryUser(username, getTimezone(getIP(r)))

	// Compare inputted password to the password in the database. If they're the same, return nil.
	var password_compare = bcrypt.CompareHashAndPassword([]byte(users.Password), []byte(password))

	if password_compare == nil {
		session := sessions.Start(w, r)
		session.Set("username", users.Username)
		session.Set("user_id", users.ID)
		stmt, err := db.Prepare("INSERT INTO sessions (id, user) VALUES (?, ?)")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		stmt.Exec(session.ID(), users.ID)
		stmt, err = db.Prepare("UPDATE users SET last_seen = NOW(), ip = ? where id = ?")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		host, _, _ := net.SplitHostPort(getIP(r))
		stmt.Exec(host, users.ID)
		loginToken := generateLoginToken()
		stmt, err = db.Prepare("INSERT INTO login_tokens (value, user) VALUES(?, ?)")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		stmt.Exec(loginToken, users.ID)

		if settings.Webhooks.Enabled && len(settings.Webhooks.Logins) > 0 {
			var username string
			username = users.Nickname
			if users.Nickname != users.Username {
				username += " (" + users.Username + ")"
			}
			ip, _, err := net.SplitHostPort(getIP(r))
			data := map[string]interface{}{
				"content": fmt.Sprintf("%s just logged in from the IP %s.\n%s", escapeMarkdown(username), ip, getHostname(r.Host)+"/users/"+url.PathEscape(users.Username)),
			}
			jsonBytes, err := json.Marshal(data)
			if err != nil {
				fmt.Println(err.Error())
			}
			_, err = http.Post(settings.Webhooks.Logins, "application/json", bytes.NewBuffer(jsonBytes))
			if err != nil {
				fmt.Println(err.Error())
			}
		}

		cookie := http.Cookie{Name: "indigo-auth", Value: loginToken, Expires: time.Now().Add(365 * 24 * time.Hour)}
		http.SetCookie(w, &cookie)
		http.Redirect(w, r, "/", 302)
	} else {
		http.Redirect(w, r, "/login?error=1", 302)
	}
}

// Log a user out.
func logout(w http.ResponseWriter, r *http.Request) {
	session := sessions.Start(w, r)
	userID := session.Get("user_id")
	session.Clear()
	sessions.Destroy(w, r)
	indigoAuth, err := r.Cookie("indigo-auth")
	if err == nil {
		stmt, _ := db.Prepare("DELETE FROM login_tokens WHERE value = ?")
		stmt.Exec(indigoAuth.Value)
		cookie := http.Cookie{Path: "/", Name: "indigo-auth", MaxAge: -1, Expires: time.Now().Add(-100 * time.Hour)}
		http.SetCookie(w, &cookie)
	}
	if settings.ForceLogins {
		http.Redirect(w, r, "/login", 302)
	} else {
		http.Redirect(w, r, "/", 302)
	}

	var msg wsMessage
	msg.Type = "refresh"
	for client := range clients {
		//clients[client].Mutex.Lock()
		if clients[client].UserID == userID {
			writeWs(clients[client], client, msg)
			client.Close()
			delete(clients, client)
		}
		//clients[client].Mutex.Unlock()
	}
}

// Import a user's posts from another social network via an external API.
func migratePosts(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	vars := mux.Vars(r)
	migration_id := vars["id"]
	var script string
	var password_required bool
	db.QueryRow("SELECT script, password_required FROM migrations WHERE id = ? AND is_rm = 0", migration_id).Scan(&script, &password_required)
	if len(script) == 0 {
		handle404(w, r)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")
	if len(username) == 0 {
		http.Error(w, "You must specify a username.", http.StatusBadRequest)
		return
	}
	if password_required && len(password) == 0 {
		http.Error(w, "You must specify a password.", http.StatusBadRequest)
		return
	}

	var importCount int
	err = db.QueryRow("SELECT COUNT(*) FROM imports WHERE username = ? AND migration = ? AND user != ?", username, migration_id, currentUser.ID).Scan(&importCount)
	if importCount > 0 {
		var importUsername string
		db.QueryRow("SELECT users.username FROM users LEFT JOIN imports ON user = users.id WHERE imports.username = ? AND migration = ? AND user != ? LIMIT 1", username, migration_id, currentUser.ID).Scan(&importUsername)
		http.Error(w, "A user by the username "+importUsername+" has already created a post import request for that user.\nIf you have an issue with this, contact that user or a staff member.", http.StatusBadRequest)
		return
	}

	data := url.Values{}
	data.Set("username", username)
	data.Set("password", password)
	resp, err := http.Post(script, "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var jsonBody migration
	err = json.Unmarshal(body, &jsonBody)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if jsonBody.Success != 1 {
		http.Error(w, jsonBody.Error, resp.StatusCode)
		return
	}

	import_id := -1
	db.QueryRow("SELECT id FROM imports WHERE username = ? AND migration = ? AND user = ?", username, migration_id, currentUser.ID).Scan(&import_id)
	if import_id == -1 {
		stmt, _ := db.Prepare("INSERT INTO imports (user, migration, username) VALUES (?, ?, ?)")
		_, err = stmt.Exec(currentUser.ID, migration_id, username)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		db.QueryRow("SELECT id FROM imports WHERE username = ? AND migration = ? AND user = ?", username, migration_id, currentUser.ID).Scan(&import_id)
	}

	stmt, err := db.Prepare("INSERT INTO posts (migration, import_id, migrated_id, created_by, migrated_community, created_at, edited_at, feeling, body, image, attachment_type, url, url_type, is_spoiler, is_rm_by_admin, post_type, community_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	stmt2, err := db.Prepare("INSERT INTO migrated_communities (migration, migrated_id, title, icon) VALUES (?, ?, ?, ?)")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	for i := range jsonBody.Posts {
		post := jsonBody.Posts[i]
		var existsCount int
		db.QueryRow("SELECT COUNT(*) FROM posts WHERE migrated_id = ? AND migration = ? AND is_rm = 0", post.ID, migration_id).Scan(&existsCount)
		if existsCount == 0 {
			if post.EditedAt == "0000-00-00 00:00:00" {
				post.EditedAt = post.CreatedAt
			}
			urlType := 0
			if len(post.URL) > 0 {
				matched := youtube.FindAllStringSubmatch(post.URL, 1)
				if len(matched) > 0 {
					post.URL = matched[0][1]
					urlType = 1
				} else {
					matched = spotify.FindAllStringSubmatch(post.URL, 1)
					if len(matched) > 0 {
						post.URL = matched[0][1]
						urlType = 2
					} else {
						matched = soundcloud.FindAllStringSubmatch(post.URL, 1)
						if len(matched) > 0 {
							post.URL = matched[0][0]
							urlType = 3
						}
					}
				}
			}
			if utf8.RuneCountInString(post.Body) > 2000 {
				runes := []rune(post.Body) // What is this, fucking RuneScape!?
				post.Body = string(runes[0:1997]) + "..."
			}
			_, err = stmt.Exec(migration_id, import_id, post.ID, currentUser.ID, post.CommunityID, post.CreatedAt, post.EditedAt, post.Feeling, post.Body, post.Image, post.AttachmentType, post.URL, urlType, post.IsSpoiler, post.IsRMByAdmin, post.PostType)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}
	for i := range jsonBody.Communities {
		community := jsonBody.Communities[i]
		var existsCount int
		db.QueryRow("SELECT COUNT(*) FROM migrated_communities WHERE migrated_id = ? AND migration = ?", community.ID, migration_id).Scan(&existsCount)
		if existsCount == 0 {
			_, err = stmt2.Exec(migration_id, community.ID, community.Title, community.Icon)
			if err != nil {
				fmt.Println(community)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}
}

// Send a friend request to a user.
func newFriendRequest(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	vars := mux.Vars(r)
	username := vars["username"]
	message := r.FormValue("body")
	var user_id int
	var requested int
	err := db.QueryRow("SELECT id FROM users WHERE username = ?", username).Scan(&user_id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if user_id == 0 {
		http.Error(w, "That user does not exist.", http.StatusBadRequest)
		return
	}

	err = db.QueryRow("SELECT COUNT(*) FROM friend_requests WHERE request_by = ? AND request_to = ?", user_id, currentUser.ID).Scan(&requested)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if requested == 1 {
		http.Error(w, "You have already sent a friend request to this user.", http.StatusBadRequest)
		return
	}

	if checkIfEitherBlocked(user_id, currentUser.ID) && currentUser.Level == 0 {
		http.Error(w, "You're not allowed to do that.", http.StatusBadRequest)
		return
	}

	stmt, err := db.Prepare("INSERT INTO friend_requests (request_to, request_by, message) VALUES (?, ?, ?)")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	stmt.Exec(&user_id, &currentUser.ID, &message)

	var msg wsMessage
	msg.Type = "notif"
	var notifCount int
	var friendRequests int
	db.QueryRow("SELECT COUNT(*) FROM notifications WHERE notif_to = ? AND merged IS NULL AND notif_read = 0", user_id).Scan(&notifCount)
	db.QueryRow("SELECT COUNT(*) FROM friend_requests WHERE request_to = ? AND request_read = 0", user_id).Scan(&friendRequests)
	msg.Content = strconv.Itoa(notifCount + friendRequests)
	for client := range clients {
		//clients[client].Mutex.Lock()
		if clients[client].UserID == user_id {
			err := writeWs(clients[client], client, msg)
			if err != nil {
				client.Close()
				delete(clients, client)
			}
		}
		//clients[client].Mutex.Unlock()
	}
}

// Reject a friend request.
func rejectFriendRequest(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	vars := mux.Vars(r)
	username := vars["username"]
	var user_id int

	err := db.QueryRow("SELECT id FROM users WHERE username = ?", username).Scan(&user_id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if user_id == 0 {
		http.Error(w, "That user does not exist.", http.StatusBadRequest)
		return
	}

	stmt, err := db.Prepare("DELETE FROM friend_requests WHERE request_by = ? AND request_to = ?")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	stmt.Exec(&user_id, &currentUser.ID)
}

// Report a comment.
func reportComment(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	vars := mux.Vars(r)
	comment_id := vars["id"]
	var sex int

	err := db.QueryRow("SELECT COUNT(*) FROM comments WHERE id = ? AND created_by != ? AND is_rm = 0", comment_id, currentUser.ID).Scan(&sex)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if sex < 0 {
		http.Error(w, "The comment could not be found.", http.StatusNotFound)
		return
	}

	err = db.QueryRow("SELECT COUNT(*) FROM reports WHERE type = 1 AND pid = ? AND user = ?", comment_id, currentUser.ID).Scan(&sex)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if sex > 0 {
		return
	}
	reason := r.FormValue("type")
	if reason == "spoiler" {
		reason = "0"
	}
	message := r.FormValue("body")

	stmt, err := db.Prepare("INSERT INTO reports (type, pid, user, reason, message) VALUES (1, ?, ?, ?, ?)")
	stmt.Exec(&comment_id, &currentUser.ID, &reason, &message)

	if settings.Webhooks.Enabled && len(settings.Webhooks.Reports) > 0 {
		reasonInt, _ := strconv.Atoi(reason)
		content := "New report from **" + escapeMarkdown(currentUser.Nickname) + "**.\nReason: " + settings.ReportReasons[reasonInt].Name + "\n"
		if len(message) > 0 {
			content += "Message: " + escapeMarkdown(message) + "\n"
		}
		content += "Comment link: " + getHostname(r.Host) + "/comments/" + comment_id
		data := map[string]interface{}{
			"content": content,
		}
		jsonBytes, err := json.Marshal(data)
		if err != nil {
			fmt.Println(err.Error())
		}
		_, err = http.Post(settings.Webhooks.Reports, "application/json", bytes.NewBuffer(jsonBytes))
		if err != nil {
			fmt.Println(err.Error())
		}
	}
}

// Delete a post/comment/user from a report.
func reportIgnore(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}
	if currentUser.Level < 1 {
		http.Redirect(w, r, "/", 301)
		return
	}

	vars := mux.Vars(r)
	reportID := vars["id"]
	var asdasofigheioioeiwjgekwjwjekjegijegwjeijeijweiojjofijqijqjiwqwfijqwijw int
	err = db.QueryRow("SELECT COUNT(*) FROM reports WHERE id = ?", reportID).Scan(&asdasofigheioioeiwjgekwjwjekjegijegwjeijeijweiojjofijqijqjiwqwfijqwijw)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if asdasofigheioioeiwjgekwjwjekjegijegwjeijeijweiojjofijqijqjiwqwfijqwijw == 0 {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, err = db.Exec("UPDATE reports SET is_rm = 1 WHERE id = ?", reportID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// Report a post.
func reportPost(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	vars := mux.Vars(r)
	post_id := vars["id"]
	var sex int

	err := db.QueryRow("SELECT COUNT(*) FROM posts WHERE id = ? AND created_by != ? AND is_rm = 0 AND is_rm_by_admin = 0", post_id, currentUser.ID).Scan(&sex)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if sex < 0 {
		http.Error(w, "The post could not be found.", http.StatusNotFound)
		return
	}

	err = db.QueryRow("SELECT COUNT(*) FROM reports WHERE type = 0 AND pid = ? AND user = ?", post_id, currentUser.ID).Scan(&sex)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if sex > 0 {
		return
	}
	reason := r.FormValue("type")
	if reason == "spoiler" {
		reason = "0"
	}
	message := r.FormValue("body")

	stmt, err := db.Prepare("INSERT INTO reports (type, pid, user, reason, message) VALUES (0, ?, ?, ?, ?)")
	stmt.Exec(&post_id, &currentUser.ID, &reason, &message)

	if settings.Webhooks.Enabled && len(settings.Webhooks.Reports) > 0 {
		reasonInt, _ := strconv.Atoi(reason)
		content := "New report from **" + escapeMarkdown(currentUser.Nickname) + "**.\nReason: " + settings.ReportReasons[reasonInt].Name + "\n"
		if len(message) > 0 {
			content += "Message: " + escapeMarkdown(message) + "\n"
		}
		content += "Post link:" + getHostname(r.Host) + "/posts/" + post_id
		data := map[string]interface{}{
			"content": content,
		}
		jsonBytes, err := json.Marshal(data)
		if err != nil {
			fmt.Println(err.Error())
		}
		_, err = http.Post(settings.Webhooks.Reports, "application/json", bytes.NewBuffer(jsonBytes))
		if err != nil {
			fmt.Println(err.Error())
		}
	}
}

// Report a user.
func reportUser(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	vars := mux.Vars(r)
	username := vars["username"]
	var sex int

	err := db.QueryRow("SELECT id FROM users WHERE username = ? AND id != ?", username, currentUser.ID).Scan(&sex)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if sex == 0 {
		fmt.Println(sex)
		http.Error(w, "The user could not be found.", http.StatusNotFound)
		return
	}

	var reportCount int
	err = db.QueryRow("SELECT COUNT(*) FROM reports WHERE type = 2 AND pid = ? AND user = ?", sex, currentUser.ID).Scan(&reportCount)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if reportCount > 0 {
		return
	}
	reason := r.FormValue("type")
	if reason == "spoiler" {
		reason = "0"
	}
	message := r.FormValue("body")

	stmt, err := db.Prepare("INSERT INTO reports (type, pid, user, reason, message) VALUES (2, ?, ?, ?, ?)")
	stmt.Exec(&sex, &currentUser.ID, &reason, &message)

	if settings.Webhooks.Enabled && len(settings.Webhooks.Reports) > 0 {
		reasonInt, _ := strconv.Atoi(reason)
		content := fmt.Sprintf("New report from **%s**.\nReason: %s\nMessage: %s\nUser link: %s/users/%s", escapeMarkdown(currentUser.Nickname), settings.ReportReasons[reasonInt].Name, escapeMarkdown(message), getHostname(r.Host), url.PathEscape(username))
		data := map[string]interface{}{
			"content": content,
		}
		jsonBytes, err := json.Marshal(data)
		if err != nil {
			fmt.Println(err.Error())
		}
		_, err = http.Post(settings.Webhooks.Reports, "application/json", bytes.NewBuffer(jsonBytes))
		if err != nil {
			fmt.Println(err.Error())
		}
	}
}

// Reset a user's password.
func resetPassword(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}

	var data map[string]interface{}
	var userID int
	var username string
	vars := mux.Vars(r)
	token := vars["token"]
	err = db.QueryRow("SELECT users.id, username FROM users LEFT JOIN password_resets ON users.id = user WHERE token = ?", token).Scan(&userID, &username)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(username) == 0 {
		data = map[string]interface{}{
			"Title":       "Reset Password",
			"CurrentUser": currentUser,
			"Action":      "error",
			"Error":       "The token you specified was not found in our database.",
		}
	} else if r.Method == "POST" {
		password := r.FormValue("password")
		confirm := r.FormValue("confirm")
		if password != confirm {
			w.WriteHeader(http.StatusBadRequest)
			data = map[string]interface{}{
				"Title":       "Reset Password",
				"CurrentUser": currentUser,
				"Action":      "reset",
				"Error":       "Your password and confirm password must match.",
				"CSRFField":   csrf.TemplateField(r),
			}
		} else if len(password) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			data = map[string]interface{}{
				"Title":       "Reset Password",
				"CurrentUser": currentUser,
				"Action":      "reset",
				"Error":       "You must enter a password.",
				"CSRFField":   csrf.TemplateField(r),
			}
		} else {
			password, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			stmt, _ := db.Prepare("UPDATE users SET password = ? WHERE id = ?")
			stmt.Exec(password, userID)
			stmt, _ = db.Prepare("DELETE FROM password_resets WHERE token = ?")
			stmt.Exec(token)
			stmt, _ = db.Prepare("DELETE FROM login_tokens WHERE user = ?")
			stmt.Exec(userID)

			session_rows, err := db.Query("SELECT id FROM sessions WHERE user = ?", userID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			for session_rows.Next() {
				var sessionID string
				session_rows.Scan(&sessionID)
				sessions.DestroyByID(sessionID)
			}
			session_rows.Close()
			stmt, _ = db.Prepare("DELETE FROM sessions WHERE user = ?")
			stmt.Exec(userID)

			var msg wsMessage
			msg.Type = "refresh"
			for client := range clients {
				//clients[client].Mutex.Lock()
				if clients[client].UserID == userID {
					writeWs(clients[client], client, msg)
					client.Close()
					delete(clients, client)
				}
				//clients[client].Mutex.Unlock()
			}

			currentUser, success = doSession(w, r)
			if !success {
				return
			}
			data = map[string]interface{}{
				"Title":       "Reset Password",
				"CurrentUser": currentUser,
				"Action":      "success",
				"Username":    username,
			}
		}
	} else {
		data = map[string]interface{}{
			"Title":       "Reset Password",
			"CurrentUser": currentUser,
			"Action":      "reset",
			"Username":    username,
			"CSRFField":   csrf.TemplateField(r),
		}
	}

	err := templates.ExecuteTemplate(w, "reset.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Rollback a post import.
func rollbackImport(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	vars := mux.Vars(r)
	import_id := vars["id"]
	_, err = db.Exec("UPDATE posts SET is_rm = 1 WHERE import_id = ?", import_id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, err = db.Exec("DELETE FROM imports WHERE id = ? AND user = ?", import_id, currentUser.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Send a message.
func sendMessage(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	user_id := currentUser.ID
	conversation_id := r.FormValue("conversation")
	post_type := r.FormValue("post_type")
	body := r.FormValue("body")
	painting := r.FormValue("painting")
	if post_type == "1" {
		body = painting
	}
	image := r.FormValue("image")
	attachment_type := r.FormValue("attachment_type")
	messageURL := ""
	url_type := 0
	feeling := r.FormValue("feeling_id")

	var otherUserID int
	var target int
	err := db.QueryRow("SELECT if(source = ?, target, source), target FROM conversations WHERE conversations.id = ? AND if(target = 0, ?, if(source = ?, source, target)) = ? AND conversations.is_rm = 0", user_id, conversation_id, user_id, user_id, user_id).Scan(&otherUserID, &target)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if otherUserID == 0 && target != 0 {
		http.Error(w, "The conversation could not be found.", http.StatusBadRequest)
		return
	}
	if target == 0 {
		var userCount int
		err = db.QueryRow("SELECT COUNT(*) FROM group_members WHERE user = ? AND conversation = ?", currentUser.ID, conversation_id).Scan(&userCount)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if userCount == 0 {
			http.Error(w, "You're not a member of that conversation.", http.StatusBadRequest)
			return
		}
	}

	if utf8.RuneCountInString(body) > 2000 {
		http.Error(w, "Your message is too long. (2000 characters maximum)", http.StatusBadRequest)
		return
	}
	if len(body) == 0 && len(image) == 0 {
		http.Error(w, "Your message is empty.", http.StatusBadRequest)
		return
	}
	if len(image) > 0 {
		imageURL := ""
		db.QueryRow("SELECT value FROM images WHERE id = ?", image).Scan(&imageURL)
		if len(imageURL) == 0 {
			http.Error(w, "Invalid image.", http.StatusBadRequest)
			return
		}
		image = imageURL
	}
	if len(attachment_type) == 0 {
		attachment_type = "0"
	}
	if len(post_type) == 0 {
		post_type = "0"
	} else if post_type == "1" {
		if len(painting) == 0 {
			http.Error(w, "You must add a drawing.", http.StatusBadRequest)
			return
		}
	} else if post_type != "0" {
		http.Error(w, "Invalid post type.", http.StatusBadRequest)
		return
	}

	if len(body) > 0 {
		matched := youtube.FindAllStringSubmatch(body, 1)
		if len(matched) > 0 {
			messageURL = matched[0][1]
			url_type = 1
		} else {
			matched = spotify.FindAllStringSubmatch(body, 1)
			if len(matched) > 0 {
				messageURL = matched[0][1]
				url_type = 2
			} else {
				matched = soundcloud.FindAllStringSubmatch(body, 1)
				if len(matched) > 0 {
					messageURL = "https://" + matched[0][0]
					url_type = 3
				}
			}
		}
	}

	var msg_read bool
	if target == 0 {
		msg_read = true
	} else {
		msg_read = false
	}

	stmt, err := db.Prepare("INSERT messages SET created_by = ?, conversation_id = ?, body = ?, image = ?, attachment_type = ?, url = ?, url_type = ?, post_type = ?, feeling = ?, msg_read = ?")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// If there's no errors, we can go ahead and execute the statement.
	_, err = stmt.Exec(user_id, conversation_id, body, image, attachment_type, messageURL, url_type, post_type, feeling, msg_read)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Cool, it worked! Now we have to retrieve what we just grabbed so we can show it to the user.
	var messages = message{}
	var timestamp time.Time
	err = db.QueryRow("SELECT messages.id, created_at, post_type, avatar FROM messages LEFT JOIN users ON created_by = users.id WHERE conversation_id = ? AND created_by = ? ORDER BY messages.id DESC LIMIT 1", conversation_id, user_id).Scan(&messages.ID, &timestamp, &messages.PostType, &messages.ByAvatar)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	messages.Date = humanTiming(timestamp, currentUser.Timezone)
	messages.DateUnix = timestamp.Unix()
	messages.Feeling, err = strconv.Atoi(feeling)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	messages.BodyText = body
	messages.Body = parseBody(body, false, true)
	messages.Image = image
	messages.AttachmentType, err = strconv.Atoi(attachment_type)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	messages.URL = messageURL
	messages.URLType = url_type
	messages.ByUsername = currentUser.Username
	messages.ByAvatar = getAvatar(messages.ByAvatar, currentUser.HasMii, messages.Feeling)
	messages.ByOnline = currentUser.Online
	messages.ByHideOnline = currentUser.HideOnline
	messages.ByRoleImage = currentUser.Role.Image
	messages.ByMe = true

	err = templates.ExecuteTemplate(w, "render_message.html", messages)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var msgTpl bytes.Buffer
	messages.ByMe = false
	templates.ExecuteTemplate(&msgTpl, "render_message.html", messages)

	if target == 0 {
		_, err = db.Exec("UPDATE group_members SET unread_messages = unread_messages + 1 WHERE user != ? AND conversation = ?", currentUser.ID, conversation_id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	var msg wsMessage
	msg.Type = "message"
	msg.Content = msgTpl.String()

	for client := range clients {
		//clients[client].Mutex.Lock()
		var qualifies bool
		if target == 0 {
			qualifies = clients[client].OnPage == "/conversations/"+conversation_id && clients[client].UserID != currentUser.ID // not checking if the user is in the conversation or not is a SECURITY ISSUE, change this ASAP!
		} else {
			qualifies = clients[client].OnPage == "/messages/"+url.PathEscape(currentUser.Username) && clients[client].UserID == otherUserID
		}
		if qualifies {
			err := writeWs(clients[client], client, msg)
			if err != nil {
				client.Close()
				delete(clients, client)
			} else {
				db.Exec("UPDATE messages SET msg_read = 1 WHERE msg_read = 0 AND conversation_id = ? AND created_by = ?", conversation_id, user_id)
				if target == 0 {
					db.Exec("UPDATE group_members SET unread_messages = 0 WHERE conversation = ? AND user = ?", conversation_id, user_id)
				}
			}
		} else if clients[client].UserID == otherUserID || target == 0 {
			if target == 0 {
				inConversation := false
				nigga_rows, _ := db.Query("SELECT user FROM group_members WHERE conversation = ? AND user != ?", conversation_id, currentUser.ID)
				for nigga_rows.Next() {
					var currentUserID int
					nigga_rows.Scan(&currentUserID)
					if currentUserID == clients[client].UserID {
						inConversation = true
						break
					}
				}
				nigga_rows.Close()
				if !inConversation {
					continue
				}
			}
			if clients[client].OnPage == "/messages" {
				msg.Type = "messagePreview"
				var messagePreview message
				if target == 0 {
					var users []string
					user_rows, err := db.Query("SELECT nickname FROM group_members LEFT JOIN users ON user = users.id WHERE conversation = ? AND user != ? ORDER BY nickname ASC", conversation_id, clients[client].UserID)
					if err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					for user_rows.Next() {
						var user string
						user_rows.Scan(&user)
						users = append(users, user)
					}
					user_rows.Close()
					messagePreview.URL = getGroupName(users)
					messagePreview.URLType = 1
					messagePreview.ByUsername = conversation_id
				} else {
					messagePreview.URL = currentUser.Nickname
					messagePreview.URLType = 0
					messagePreview.ByUsername = messages.ByUsername
				}
				messagePreview.ByAvatar = messages.ByAvatar
				messagePreview.ByOnline = messages.ByOnline
				messagePreview.ByHideOnline = messages.ByHideOnline
				messagePreview.ByRoleImage = messages.ByRoleImage
				messagePreview.Date = messages.Date
				messagePreview.BodyText = body
				messageJSON, _ := json.Marshal(messagePreview)
				msg.Content = string(messageJSON)
			} else {
				msg.Type = "messageNotif"
				var unread int
				db.QueryRow("SELECT COUNT(*) FROM messages LEFT JOIN conversations ON conversation_id = conversations.id WHERE (source = ? OR target = ?) AND created_by <> ? AND msg_read = 0 AND messages.is_rm = 0 AND conversations.is_rm = 0", &otherUserID, &otherUserID, &otherUserID).Scan(&unread)
				var groupUnread int
				db.QueryRow("SELECT SUM(unread_messages) FROM group_members WHERE user = ?", otherUserID).Scan(&groupUnread)
				unread += groupUnread
				msg.Content = strconv.Itoa(unread)
			}
			err := writeWs(clients[client], client, msg)
			if err != nil {
				client.Close()
				delete(clients, client)
			}
		}
		//clients[client].Mutex.Unlock()
	}
}

// Show a user's account settings.
func showAccountSettings(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	var groupPermissions bool
	err := db.QueryRow("SELECT group_permissions FROM users WHERE id = ?", currentUser.ID).Scan(&groupPermissions)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	accountSettings := [5]bool{currentUser.YeahNotifications, currentUser.HideOnline, currentUser.HideLastSeen, groupPermissions, currentUser.WebsocketsEnabled}
	accountSettingsJSON, _ := json.Marshal(accountSettings)

	w.Header().Set("Content-Type", "application/json")
	w.Write(accountSettingsJSON)
}

// Show the Activity Feed.
func showActivityFeed(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
	}

	pjax := r.Header.Get("X-PJAX") == ""
	friendCount, followingCount, followerCount := setupSidebarStatus(currentUser.ID)
	repost := r.FormValue("repost")
	var rp repostPreview

	if len(repost) > 0 {
		repost_row, err := db.Query("SELECT posts.id, nickname, body, post_type FROM posts LEFT JOIN users ON users.id = created_by WHERE posts.id = ? AND is_rm = 0 AND is_rm_by_admin = 0 AND users.id NOT IN (SELECT if(source = ?, target, source) FROM blocks WHERE (source = ? AND target = users.id) OR (source = users.id AND target = ?)) AND IF(created_by = ?, true, LOWER(body) NOT REGEXP LOWER(?)) AND (privacy = 0 OR (privacy IN (1, 2, 3, 4) AND (SELECT COUNT(*) FROM friendships WHERE source = ? AND target = created_by OR source = created_by AND target = ? LIMIT 1) = 1) OR (privacy IN (1, 3, 5, 6) AND (SELECT COUNT(*) FROM follows WHERE follow_to = created_by AND follow_by = ? LIMIT 1) = 1) OR (privacy IN (1, 2, 5, 7) AND (SELECT COUNT(*) FROM follows WHERE follow_to = ? AND follow_by = created_by) = 1) OR (privacy = 8 AND ? > 0) OR created_by = ?) LIMIT 1", repost, currentUser.ID, currentUser.ID, currentUser.ID, currentUser.ID, escapeForbiddenKeywords(currentUser.ForbiddenKeywords), currentUser.ID, currentUser.ID, currentUser.ID, currentUser.ID, currentUser.Level, currentUser.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if repost_row.Next() {
			err = repost_row.Scan(&rp.ID, &rp.Nickname, &rp.Text, &rp.PostType)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			rp.Text = parsePreview(rp.Text, rp.PostType, false)
		}
		repost_row.Close()
	}

	if r.Header.Get("X-Requested-With") == "XMLHttpRequest" && pjax {
		offset, _ := strconv.Atoi(r.FormValue("offset"))
		post_rows, err := db.Query("SELECT posts.id, created_by, created_at, edited_at, feeling, body, image, attachment_type, is_spoiler, post_type, url, url_type, pinned, privacy, repost, migration, migrated_id, migrated_community, communities.id, title, icon, username, nickname, avatar, has_mh, online, hide_online, color, role FROM posts LEFT JOIN communities ON communities.id = community_id LEFT JOIN users ON users.id = created_by WHERE created_by IN (SELECT follow_to FROM follows WHERE follow_by = ?) AND is_rm = 0 AND is_rm_by_admin = 0 AND users.id NOT IN (SELECT if(source = ?, target, source) FROM blocks WHERE (source = ? AND target = users.id) OR (source = users.id AND target = ?)) AND IF(created_by = ?, true, LOWER(body) NOT REGEXP LOWER(?)) AND (privacy = 0 OR (privacy IN (1, 2, 3, 4) AND (SELECT COUNT(*) FROM friendships WHERE source = ? AND target = created_by OR source = created_by AND target = ? LIMIT 1) = 1) OR (privacy IN (1, 3, 5, 6) AND (SELECT COUNT(*) FROM follows WHERE follow_to = created_by AND follow_by = ? LIMIT 1) = 1) OR (privacy IN (1, 2, 5, 7) AND (SELECT COUNT(*) FROM follows WHERE follow_to = ? AND follow_by = created_by) = 1) OR (privacy = 8 AND ? > 0) OR created_by = ?) ORDER BY posts.created_at DESC, posts.id DESC LIMIT 20 OFFSET ?", currentUser.ID, currentUser.ID, currentUser.ID, currentUser.ID, currentUser.ID, escapeForbiddenKeywords(currentUser.ForbiddenKeywords), currentUser.ID, currentUser.ID, currentUser.ID, currentUser.ID, currentUser.Level, currentUser.ID, offset)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var posts []*post
		for post_rows.Next() {
			var row = &post{}

			err = post_rows.Scan(&row.ID, &row.CreatedBy, &row.CreatedAtTime, &row.EditedAtTime, &row.Feeling, &row.BodyText, &row.Image, &row.AttachmentType, &row.IsSpoiler, &row.PostType, &row.URL, &row.URLType, &row.Pinned, &row.Privacy, &row.RepostID, &row.MigrationID, &row.MigratedID, &row.MigratedCommunity, &row.CommunityID, &row.CommunityName, &row.CommunityIcon, &row.PosterUsername, &row.PosterNickname, &row.PosterIcon, &row.PosterHasMii, &row.PosterOnline, &row.PosterHideOnline, &row.PosterColor, &row.PosterRoleID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			row = setupPost(row, currentUser, 2, 0)
			posts = append(posts, row)
		}
		post_rows.Close()
		offset += 20
		var data = map[string]interface{}{
			"Offset": offset,
			"Posts":  posts,
		}
		err = templates.ExecuteTemplate(w, "activity.html", data)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		var data = map[string]interface{}{
			"Title":          "Activity Feed",
			"Pjax":           pjax,
			"CurrentUser":    currentUser,
			"FriendCount":    friendCount,
			"FollowingCount": followingCount,
			"FollowerCount":  followerCount,
			"Repost":         rp,
			"MaxUploadSize":  settings.ImageHost.MaxUploadSize,
		}
		err := templates.ExecuteTemplate(w, "activity_loading.html", data)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}

// Show the admin dashboard.
func showAdminDashboard(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}
	if currentUser.Level < 1 {
		http.Redirect(w, r, "/", 301)
		return
	}

	pjax := r.Header.Get("X-PJAX") == ""
	offset, _ := strconv.Atoi(r.FormValue("offset"))

	report_rows, err := db.Query("SELECT posts.id, created_by, community_id, created_at, edited_at, feeling, body, image, attachment_type, is_spoiler, post_type, url, url_type, pinned, privacy, repost, migration, migrated_id, migrated_community, posts.is_rm, is_rm_by_admin, username, nickname, avatar, has_mh, online, hide_online, color, role, title, icon, rm, nigga, posts.type, reports.id, reports.type, message, reason, user FROM (SELECT posts.id, posts.created_by, posts.community_id, posts.created_at, posts.edited_at, posts.feeling, posts.body, posts.image, posts.attachment_type, posts.is_spoiler, posts.post_type, posts.url, posts.url_type, posts.pinned, posts.privacy, repost, migration, migrated_id, migrated_community, posts.is_rm, posts.is_rm_by_admin, users.username, users.nickname, users.avatar, users.has_mh, users.online, users.hide_online, users.color, users.role, title, icon, rm, 0 nigga, 0 type FROM posts LEFT JOIN users ON posts.created_by = users.id LEFT JOIN communities ON community_id = communities.id UNION SELECT comments.id, comments.created_by, post, comments.created_at, comments.edited_at, comments.feeling, comments.body, comments.image, comments.attachment_type, comments.is_spoiler, comments.post_type, comments.url, comments.url_type, comments.pinned, op.privacy, 0, 0, 0, 0, comments.is_rm, comments.is_rm_by_admin, creator.username, creator.nickname, creator.avatar, creator.has_mh, creator.online, creator.hide_online, creator.color, creator.role, poster.nickname, poster.avatar, op.is_rm, poster.has_mh, 1 FROM comments LEFT JOIN posts AS op ON post = op.id LEFT JOIN users AS creator ON comments.created_by = creator.id LEFT JOIN users AS poster ON op.created_by = poster.id) posts LEFT JOIN reports ON pid = posts.id AND reports.type = posts.type WHERE reports.is_rm = 0 AND posts.is_rm = 0 AND is_rm_by_admin = 0 ORDER BY reports.id DESC LIMIT 25 OFFSET ?", offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var reports []report

	for report_rows.Next() {
		var row = &post{}
		var report = report{}
		var communityHasMii bool
		var onComment bool

		err = report_rows.Scan(&row.ID, &row.CreatedBy, &row.CommunityID, &row.CreatedAtTime, &row.EditedAtTime, &row.Feeling, &row.BodyText, &row.Image, &row.AttachmentType, &row.IsSpoiler, &row.PostType, &row.URL, &row.URLType, &row.Pinned, &row.Privacy, &row.RepostID, &row.MigrationID, &row.MigratedID, &row.MigratedCommunity, &row.IsRM, &row.IsRMByAdmin, &row.PosterUsername, &row.PosterNickname, &row.PosterIcon, &row.PosterHasMii, &row.PosterOnline, &row.PosterHideOnline, &row.PosterColor, &row.PosterRoleID, &row.CommunityName, &row.CommunityIcon, &row.CommunityRM, &communityHasMii, &onComment, &report.ID, &report.Type, &report.Message, &report.Reason, &report.ByID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if len(report.Message) == 0 {
			report.Message = settings.ReportReasons[report.Reason].Name
		}
		db.QueryRow("SELECT username, nickname, color FROM users WHERE id = ?", report.ByID).Scan(&report.ByUsername, &report.ByNickname, &report.ByColor)

		if onComment {
			row.CommunityIcon = getAvatar(row.CommunityIcon, communityHasMii, 0)
			row.CommunityName = "Comment on " + row.CommunityName + "'s Post"
			row.CommentCount = -1
		}
		row = setupPost(row, currentUser, 3, 2)
		report.Post = row
		reports = append(reports, report)
	}
	report_rows.Close()

	offset += 25

	var data = map[string]interface{}{
		"Title":       "Admin Dashboard",
		"Pjax":        pjax,
		"Offset":      offset,
		"CurrentUser": currentUser,
		"Admin":       admin,
		"Reports":     reports,
	}
	err = templates.ExecuteTemplate(w, "dashboard.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Show the list of admin managers.
func showAdminManagerList(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}
	if currentUser.Level < admin.Manage.MinimumLevel {
		http.Redirect(w, r, "/", 301)
		return
	}

	pjax := r.Header.Get("X-PJAX") == ""

	var data = map[string]interface{}{
		"Title":       "Manage",
		"Pjax":        pjax,
		"CurrentUser": currentUser,
		"Admin":       admin,
	}
	err = templates.ExecuteTemplate(w, "manage.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Show the admin settings.
func showAdminSettings(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}
	if currentUser.Level < admin.Settings.MinimumLevel {
		http.Redirect(w, r, "/", 301)
		return
	}

	if r.Method == "POST" {
		settings.ImageHost.Provider = r.FormValue("imagehost_provider")
		settings.ImageHost.Username = r.FormValue("imagehost_username")
		settings.ImageHost.UploadPreset = r.FormValue("imagehost_uploadpreset")
		settings.ImageHost.APIEndpoint = r.FormValue("imagehost_apiendpoint")
		settings.ImageHost.MaxUploadSize = r.FormValue("imagehost_maxuploadsize")

		if r.FormValue("recaptcha_enabled") == "1" {
			settings.ReCAPTCHA.Enabled = true
		} else {
			settings.ReCAPTCHA.Enabled = false
		}
		settings.ReCAPTCHA.SiteKey = r.FormValue("recaptcha_sitekey")
		settings.ReCAPTCHA.SecretKey = r.FormValue("recaptcha_secretkey")

		if r.FormValue("webhooks_enabled") == "1" {
			settings.Webhooks.Enabled = true
		} else {
			settings.Webhooks.Enabled = false
		}
		settings.Webhooks.Reports = r.FormValue("webhooks_reports")
		settings.Webhooks.Signups = r.FormValue("webhooks_signups")
		settings.Webhooks.Logins = r.FormValue("webhooks_logins")

		if r.FormValue("smtp_enabled") == "1" {
			settings.SMTP.Enabled = true
		} else {
			settings.SMTP.Enabled = false
		}
		settings.SMTP.Hostname = r.FormValue("smtp_hostname")
		settings.SMTP.Port = r.FormValue("smtp_port")
		settings.SMTP.Email = r.FormValue("smtp_email")
		settings.SMTP.Password = r.FormValue("smtp_password")

		if r.FormValue("proxy") == "1" {
			settings.Proxy = true
		} else {
			settings.Proxy = false
		}
		if r.FormValue("forcelogins") == "1" {
			settings.ForceLogins = true
		} else {
			settings.ForceLogins = false
		}
		if r.FormValue("allowsignups") == "1" {
			settings.AllowSignups = true
		} else {
			settings.AllowSignups = false
		}
		settings.DefaultTimezone = r.FormValue("defaulttimezone")
		settings.EmoteLimit, err = strconv.Atoi(r.FormValue("emotelimit"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		settingsJSON, err := json.MarshalIndent(settings, "", "	")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		err = ioutil.WriteFile("config.json", settingsJSON, 0644)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	pjax := r.Header.Get("X-PJAX") == ""
	var data = map[string]interface{}{
		"Title":       "Admin Settings",
		"Pjax":        pjax,
		"CurrentUser": currentUser,
		"Admin":       admin,
		"Settings":    settings,
	}
	err = templates.ExecuteTemplate(w, "settings.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Show a post's full comment section.
func showAllComments(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}

	vars := mux.Vars(r)
	post_id := vars["id"]

	var createdBy int
	err := db.QueryRow("SELECT created_by FROM posts WHERE id = ? AND is_rm = 0 AND is_rm_by_admin = 0", post_id).Scan(&createdBy)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if createdBy == 0 {
		http.Error(w, "The post could not be found.", http.StatusNotFound)
		return
	}

	comment_rows, _ := db.Query("SELECT comments.id, created_by, created_at, edited_at, feeling, body, image, attachment_type, is_spoiler, post_type, url, url_type, pinned, is_rm_by_admin, username, nickname, avatar, has_mh, online, hide_online, color, role FROM comments LEFT JOIN users ON users.id = created_by WHERE post = ? AND is_rm = 0 AND users.id NOT IN (SELECT if(source = ?, target, source) FROM blocks WHERE (source = ? AND target = users.id) OR (source = users.id AND target = ?)) AND IF(created_by = ?, true, LOWER(body) NOT REGEXP LOWER(?)) ORDER BY created_at ASC", post_id, currentUser.ID, currentUser.ID, currentUser.ID, currentUser.ID, escapeForbiddenKeywords(currentUser.ForbiddenKeywords))
	var comments []comment
	var pinnedComments []comment

	for comment_rows.Next() {
		var row = comment{}
		var timestamp time.Time
		var editedAt time.Time
		var role int

		err = comment_rows.Scan(&row.ID, &row.CreatedBy, &timestamp, &editedAt, &row.Feeling, &row.BodyText, &row.Image, &row.AttachmentType, &row.IsSpoiler, &row.PostType, &row.URL, &row.URLType, &row.Pinned, &row.IsRMByAdmin, &row.CommenterUsername, &row.CommenterNickname, &row.CommenterIcon, &row.CommenterHasMii, &row.CommenterOnline, &row.CommenterHideOnline, &row.CommenterColor, &role)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		row.CommenterIcon = getAvatar(row.CommenterIcon, row.CommenterHasMii, row.Feeling)

		if role > 0 {
			row.CommenterRoleImage = getRoleImage(role)
		}

		row.CreatedAt = humanTiming(timestamp, currentUser.Timezone)
		row.CreatedAtUnix = timestamp.Unix()
		if editedAt.Sub(timestamp).Minutes() > 5 {
			row.EditedAt = humanTiming(editedAt, currentUser.Timezone)
			row.EditedAtUnix = editedAt.Unix()
		}
		row.Body = parseBody(row.BodyText, false, true)

		row.ByMe = row.CreatedBy == createdBy
		row.CanYeah = checkIfCanYeah(currentUser, row.CreatedBy)

		db.QueryRow("SELECT 1 FROM yeahs WHERE yeah_post = ? AND yeah_by = ? AND on_comment = 1", row.ID, currentUser.ID).Scan(&row.Yeahed)
		db.QueryRow("SELECT COUNT(*) FROM yeahs WHERE yeah_post = ? AND on_comment = 1", row.ID).Scan(&row.YeahCount)

		if row.Pinned {
			pinnedComments = append(pinnedComments, row)
		} else {
			comments = append(comments, row)
		}
	}
	comment_rows.Close()

	var data = map[string]interface{}{
		"CurrentUser":    currentUser,
		"PinnedComments": pinnedComments,
		"Comments":       comments,
	}
	err = templates.ExecuteTemplate(w, "all_comments.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Show a list of all the communities.
func showAllCommunities(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}

	pjax := r.Header.Get("X-PJAX") == ""
	offset, _ := strconv.Atoi(r.FormValue("offset"))
	friendCount, followerCount, followingCount := setupSidebarStatus(currentUser.ID)

	community_rows, err := db.Query("SELECT id, title, icon FROM communities WHERE rm = 0 ORDER BY id DESC LIMIT 25 OFFSET ?", offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var communities []community

	for community_rows.Next() {
		var row = community{}
		err = community_rows.Scan(&row.ID, &row.Title, &row.Icon)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		communities = append(communities, row)
	}
	community_rows.Close()

	offset += 25

	var data = map[string]interface{}{
		"Title":          "All Communities",
		"Pjax":           pjax,
		"Offset":         offset,
		"FriendCount":    friendCount,
		"FollowingCount": followingCount,
		"FollowerCount":  followerCount,
		"CurrentUser":    currentUser,
		"Communities":    communities,
	}
	err = templates.ExecuteTemplate(w, "all_communities.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	return
}

// Show a user's block list.
func showBlocked(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	offset, _ := strconv.Atoi(r.FormValue("offset"))
	pjax := r.Header.Get("X-PJAX") == ""
	friendCount, followerCount, followingCount := setupSidebarStatus(currentUser.ID)

	user_rows, err := db.Query("SELECT users.id, username, nickname, avatar, has_mh, online, hide_online, color, role, created_at FROM users LEFT JOIN blocks ON users.id = target WHERE source = ? ORDER BY blocks.id DESC LIMIT 25 OFFSET ?", currentUser.ID, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var users []user

	for user_rows.Next() {
		var row = user{}
		var role int
		var timestamp time.Time
		err = user_rows.Scan(&row.ID, &row.Username, &row.Nickname, &row.Avatar, &row.HasMii, &row.Online, &row.HideOnline, &row.Color, &role, &timestamp)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		row.Avatar = getAvatar(row.Avatar, row.HasMii, 0)
		if role > 0 {
			row.Role.Image = getRoleImage(role)
		}
		row.LastSeen = humanTiming(timestamp, currentUser.Timezone)

		users = append(users, row)
	}
	user_rows.Close()

	offset += 25

	var data = map[string]interface{}{
		"Title":          "Blocked Users",
		"Pjax":           pjax,
		"Offset":         offset,
		"FriendCount":    friendCount,
		"FollowingCount": followingCount,
		"FollowerCount":  followerCount,
		"CurrentUser":    currentUser,
		"Users":          users,
	}
	err = templates.ExecuteTemplate(w, "blocked.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Show a comment.
func showComment(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}

	vars := mux.Vars(r)
	comment_id := vars["id"]
	pjax := r.Header.Get("X-PJAX") == ""

	var comments = comment{}
	var posts = post{}
	var timestamp time.Time
	var editedAt time.Time
	var yeahed string
	var role int

	db.QueryRow("SELECT comments.id, created_by, created_at, edited_at, post, feeling, body, image, attachment_type, is_spoiler, post_type, url, url_type, pinned, is_rm_by_admin, username, nickname, avatar, has_mh, online, hide_online, color, role FROM comments LEFT JOIN users ON users.id = created_by WHERE comments.id = ? AND is_rm = 0", comment_id).Scan(&comments.ID, &comments.CreatedBy, &timestamp, &editedAt, &comments.PostID, &comments.Feeling, &comments.BodyText, &comments.Image, &comments.AttachmentType, &comments.IsSpoiler, &comments.PostType, &comments.URL, &comments.URLType, &comments.Pinned, &comments.IsRMByAdmin, &comments.CommenterUsername, &comments.CommenterNickname, &comments.CommenterIcon, &comments.CommenterHasMii, &comments.CommenterOnline, &comments.CommenterHideOnline, &comments.CommenterColor, &role)
	if len(string(comments.CommenterUsername)) == 0 {
		handle404(w, r)
		return
	}

	comments.CommenterIcon = getAvatar(comments.CommenterIcon, comments.CommenterHasMii, comments.Feeling)
	if role > 0 {
		comments.CommenterRoleImage, comments.CommenterRoleOrganization = getRoleImageAndOrganization(role)
	}

	comments.CreatedAt = humanTiming(timestamp, currentUser.Timezone)
	comments.CreatedAtUnix = timestamp.Unix()
	if editedAt.Sub(timestamp).Minutes() > 5 {
		comments.EditedAt = humanTiming(editedAt, currentUser.Timezone)
		comments.EditedAtUnix = editedAt.Unix()
	}
	comments.Body = parseBody(comments.BodyText, false, true)
	if comments.CreatedBy == currentUser.ID {
		comments.ByMe = true
	}
	comments.CanYeah = checkIfCanYeah(currentUser, comments.CreatedBy)

	db.QueryRow("SELECT feeling, body, privacy, post_type, is_rm | is_rm_by_admin, nickname, avatar, has_mh, communities.id, title, icon, rm FROM posts INNER JOIN users ON users.id = posts.created_by INNER JOIN communities ON communities.id = community_id WHERE posts.id = ? AND (privacy = 0 OR (privacy IN (1, 2, 3, 4) AND (SELECT COUNT(*) FROM friendships WHERE source = ? AND target = created_by OR source = created_by AND target = ? LIMIT 1) = 1) OR (privacy IN (1, 3, 5, 6) AND (SELECT COUNT(*) FROM follows WHERE follow_to = created_by AND follow_by = ? LIMIT 1) = 1) OR (privacy IN (1, 2, 5, 7) AND (SELECT COUNT(*) FROM follows WHERE follow_to = ? AND follow_by = created_by) = 1) OR (privacy = 8 AND ? > 0) OR created_by = ?)", comments.PostID, currentUser.ID, currentUser.ID, currentUser.ID, currentUser.ID, currentUser.Level, currentUser.ID).Scan(&posts.Feeling, &posts.BodyText, &posts.Privacy, &posts.PostType, &posts.IsRM, &posts.PosterNickname, &posts.PosterIcon, &posts.PosterHasMii, &posts.CommunityID, &posts.CommunityName, &posts.CommunityIcon, &posts.CommunityRM)
	if len(posts.CommunityName) == 0 {
		handle404(w, r)
		return
	}
	posts.PosterIcon = getAvatar(posts.PosterIcon, posts.PosterHasMii, posts.Feeling)
	posts.BodyText = parsePreview(posts.BodyText, posts.PostType, posts.IsRM)

	db.QueryRow("SELECT id FROM yeahs WHERE yeah_post = ? AND yeah_by = ? AND on_comment = 1", comments.ID, currentUser.ID).Scan(&yeahed)
	if yeahed != "" {
		comments.Yeahed = true
	}

	db.QueryRow("SELECT COUNT(*) FROM yeahs WHERE yeah_post = ? AND on_comment = 1", comment_id).Scan(&comments.YeahCount)

	yeah_rows, _ := db.Query("SELECT yeahs.id, username, avatar, has_mh, role FROM yeahs LEFT JOIN users ON users.id = yeah_by WHERE yeah_post = ? AND yeah_by != ? AND on_comment = 1 ORDER BY yeahs.id DESC", comment_id, currentUser.ID)
	var yeahs []yeah

	for yeah_rows.Next() {
		var row = yeah{}
		var role int

		err = yeah_rows.Scan(&row.ID, &row.Username, &row.Avatar, &row.HasMii, &role)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		row.Avatar = getAvatar(row.Avatar, row.HasMii, comments.Feeling)
		if role > 0 {
			row.Role = getRoleImage(role)
		}

		yeahs = append(yeahs, row)
	}
	yeah_rows.Close()

	var data = map[string]interface{}{
		"Title":       comments.CommenterNickname + "'s Comment on " + posts.PosterNickname + "'s Post",
		"Pjax":        pjax,
		"CurrentUser": currentUser,
		"Comment":     comments,
		"Post":        posts,
		"Yeahs":       yeahs,
		"Reasons":     settings.ReportReasons,
	}
	err := templates.ExecuteTemplate(w, "comment.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Show a community.
func showCommunity(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}

	vars := mux.Vars(r)
	community_id := vars["id"]
	communities := QueryCommunity(community_id, false)
	if len(communities.Title) == 0 {
		handle404(w, r)
		return
	}
	pjax := r.Header.Get("X-PJAX") == ""
	offset, _ := strconv.Atoi(r.FormValue("offset"))
	query := r.URL.Query().Get("q")
	repost := r.FormValue("repost")
	var rp repostPreview

	if len(repost) > 0 {
		repost_row, err := db.Query("SELECT posts.id, nickname, body, post_type FROM posts LEFT JOIN users ON users.id = created_by WHERE posts.id = ? AND is_rm = 0 AND is_rm_by_admin = 0 AND (users.id NOT IN (SELECT if(source = ?, target, source) FROM blocks WHERE (source = ? AND target = users.id) OR (source = users.id AND target = ?)) OR ? > 0) AND IF(created_by = ?, true, LOWER(body) NOT REGEXP LOWER(?)) AND (privacy = 0 OR (privacy IN (1, 2, 3, 4) AND (SELECT COUNT(*) FROM friendships WHERE source = ? AND target = created_by OR source = created_by AND target = ? LIMIT 1) = 1) OR (privacy IN (1, 3, 5, 6) AND (SELECT COUNT(*) FROM follows WHERE follow_to = created_by AND follow_by = ? LIMIT 1) = 1) OR (privacy IN (1, 2, 5, 7) AND (SELECT COUNT(*) FROM follows WHERE follow_to = ? AND follow_by = created_by) = 1) OR (privacy = 8 AND ? > 0) OR created_by = ?) LIMIT 1", repost, currentUser.ID, currentUser.ID, currentUser.ID, currentUser.Level, currentUser.ID, escapeForbiddenKeywords(currentUser.ForbiddenKeywords), currentUser.ID, currentUser.ID, currentUser.ID, currentUser.ID, currentUser.Level, currentUser.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if repost_row.Next() {
			err = repost_row.Scan(&rp.ID, &rp.Nickname, &rp.Text, &rp.PostType)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			rp.Text = parsePreview(rp.Text, rp.PostType, false)
		}
		repost_row.Close()
	}

	var favoriteGiven bool
	if len(currentUser.Username) > 0 {
		var favorited int
		err = db.QueryRow("SELECT COUNT(*) FROM community_favorites WHERE community = ? AND favorite_by = ?", community_id, currentUser.ID).Scan(&favorited)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if favorited > 0 {
			favoriteGiven = true
		}
	}

	post_rows, err := db.Query("SELECT posts.id, created_by, posts.created_at, edited_at, feeling, body, image, attachment_type, is_spoiler, post_type, url, url_type, pinned, privacy, repost, username, nickname, avatar, has_mh, online, hide_online, color, role FROM posts INNER JOIN users ON users.id = created_by WHERE community_id = ? AND is_rm = 0 AND is_rm_by_admin = 0 AND migration = 0 AND body LIKE CONCAT('%', ?, '%') AND (users.id NOT IN (SELECT if(source = ?, target, source) FROM blocks WHERE (source = ? AND target = users.id) OR (source = users.id AND target = ?)) OR ? > 0) AND IF(created_by = ?, true, LOWER(body) NOT REGEXP LOWER(?)) AND (privacy = 0 OR (privacy IN (1, 2, 3, 4) AND (SELECT COUNT(*) FROM friendships WHERE source = ? AND target = created_by OR source = created_by AND target = ? LIMIT 1) = 1) OR (privacy IN (1, 3, 5, 6) AND (SELECT COUNT(*) FROM follows WHERE follow_to = created_by AND follow_by = ? LIMIT 1) = 1) OR (privacy IN (1, 2, 5, 7) AND (SELECT COUNT(*) FROM follows WHERE follow_to = ? AND follow_by = created_by) = 1) OR (privacy = 8 AND ? > 0) OR created_by = ?) ORDER BY pinned DESC, posts.id DESC, posts.created_at DESC LIMIT 25 OFFSET ?", community_id, query, currentUser.ID, currentUser.ID, currentUser.ID, currentUser.Level, currentUser.ID, escapeForbiddenKeywords(currentUser.ForbiddenKeywords), currentUser.ID, currentUser.ID, currentUser.ID, currentUser.ID, currentUser.Level, currentUser.ID, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var posts []*post

	for post_rows.Next() {
		var row = &post{}
		err = post_rows.Scan(&row.ID, &row.CreatedBy, &row.CreatedAtTime, &row.EditedAtTime, &row.Feeling, &row.BodyText, &row.Image, &row.AttachmentType, &row.IsSpoiler, &row.PostType, &row.URL, &row.URLType, &row.Pinned, &row.Privacy, &row.RepostID, &row.PosterUsername, &row.PosterNickname, &row.PosterIcon, &row.PosterHasMii, &row.PosterOnline, &row.PosterHideOnline, &row.PosterColor, &row.PosterRoleID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		row = setupPost(row, currentUser, 0, 0)
		posts = append(posts, row)
	}
	post_rows.Close()

	offset += 25

	var data = map[string]interface{}{
		"Title":         communities.Title,
		"Pjax":          pjax,
		"Offset":        offset,
		"Query":         query,
		"Repost":        rp,
		"CurrentUser":   currentUser,
		"Community":     communities,
		"FavoriteGiven": favoriteGiven,
		"PopularPosts":  false,
		"Posts":         posts,
		"MaxUploadSize": settings.ImageHost.MaxUploadSize,
	}
	err = templates.ExecuteTemplate(w, "communities.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Search for communities.
func showCommunitySearch(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}

	vars := mux.Vars(r)
	query := vars["search"]
	if len(query) == 0 || utf8.RuneCountInString(query) > 32 {
		handle404(w, r)
		return
	}
	pjax := r.Header.Get("X-PJAX") == ""
	offset, _ := strconv.Atoi(r.FormValue("offset"))

	user_rows, err := db.Query("SELECT id, title, description, icon FROM communities WHERE (title LIKE CONCAT('%', ?, '%') OR description LIKE CONCAT('%', ?, '%')) AND rm = 0 ORDER BY title ASC LIMIT 20 OFFSET ?", query, query, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var users []*user // Making this a user so as not to have to make another type, because ~~I'm lazy~~ it will make the server quicker.
	for user_rows.Next() {
		var row = &user{}

		err = user_rows.Scan(&row.ID, &row.Nickname, &row.Comment, &row.Avatar)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		users = append(users, row)
	}
	user_rows.Close()
	offset += 20

	friendCount, followerCount, followingCount := setupSidebarStatus(currentUser.ID)

	var data = map[string]interface{}{
		"Title":          "Search Communities",
		"Pjax":           pjax,
		"Offset":         offset,
		"CurrentUser":    currentUser,
		"FriendCount":    friendCount,
		"FollowingCount": followingCount,
		"FollowerCount":  followerCount,
		"Query":          query,
		"Action":         "/communities/search",
		"Users":          users,
	}
	err = templates.ExecuteTemplate(w, "search.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// Show the team contact page.
func showContactPage(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	pjax := r.Header.Get("X-PJAX") == ""
	friendCount, followerCount, followingCount := setupSidebarStatus(currentUser.ID)

	var data = map[string]interface{}{
		"Title":          "Contact the Team",
		"Pjax":           pjax,
		"CurrentUser":    currentUser,
		"FriendCount":    friendCount,
		"FollowingCount": followingCount,
		"FollowerCount":  followerCount,
	}
	err := templates.ExecuteTemplate(w, "contact.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// Show conversation.
func showConversation(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
	}

	vars := mux.Vars(r)
	pjax := r.Header.Get("X-PJAX") == ""
	offset, _ := strconv.Atoi(r.FormValue("offset"))
	query := r.URL.Query().Get("q")
	username := vars["username"]
	user := QueryUser(username, currentUser.Timezone)
	if len(user.Username) == 0 {
		handle404(w, r)
		return
	}
	var conversationID int
	err := db.QueryRow("SELECT id FROM conversations WHERE ((source = ? AND target = ?) OR (source = ? AND target = ?)) AND is_rm = 0", currentUser.ID, user.ID, user.ID, currentUser.ID).Scan(&conversationID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	message_rows, err := db.Query("SELECT messages.id, created_at, created_by, feeling, body, image, attachment_type, url, url_type, post_type, username, avatar, has_mh, online, hide_online, color, role FROM messages LEFT JOIN users ON users.id = created_by WHERE conversation_id = ? AND is_rm = 0 AND body LIKE CONCAT('%', ?, '%') ORDER BY messages.id DESC LIMIT 20 OFFSET ?", conversationID, query, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var messages []*message

	for message_rows.Next() {
		var row = &message{}
		var timestamp time.Time
		var role int
		var createdBy int

		err = message_rows.Scan(&row.ID, &timestamp, &createdBy, &row.Feeling, &row.BodyText, &row.Image, &row.AttachmentType, &row.URL, &row.URLType, &row.PostType, &row.ByUsername, &row.ByAvatar, &row.ByHasMii, &row.ByOnline, &row.ByHideOnline, &row.ByColor, &role)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		row.ByAvatar = getAvatar(row.ByAvatar, row.ByHasMii, row.Feeling)
		if role > 0 {
			row.ByRoleImage = getRoleImage(role)
		}

		row.Date = humanTiming(timestamp, currentUser.Timezone)
		row.DateUnix = timestamp.Unix()
		row.Body = parseBody(row.BodyText, false, true)

		if createdBy == currentUser.ID {
			row.ByMe = true
		}

		messages = append(messages, row)
	}
	message_rows.Close()

	stmt, err := db.Prepare("UPDATE messages SET msg_read = 1 WHERE msg_read = 0 AND conversation_id = ? AND created_by = ?")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, err = stmt.Exec(conversationID, user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var msg wsMessage
	msg.Type = "messageNotif"
	var unread int
	db.QueryRow("SELECT COUNT(*) FROM messages LEFT JOIN conversations ON conversation_id = conversations.id WHERE (source = ? OR target = ?) AND created_by <> ? AND msg_read = 0 AND messages.is_rm = 0 AND conversations.is_rm = 0", &currentUser.ID, &currentUser.ID, &currentUser.ID).Scan(&unread)
	var groupUnread int
	db.QueryRow("SELECT SUM(unread_messages) FROM group_members WHERE user = ?", currentUser.ID).Scan(&groupUnread)
	unread += groupUnread
	msg.Content = strconv.Itoa(unread)
	for client := range clients {
		//clients[client].Mutex.Lock()
		if clients[client].UserID == currentUser.ID {
			err := writeWs(clients[client], client, msg)
			if err != nil {
				client.Close()
				delete(clients, client)
			}
		}
		//clients[client].Mutex.Unlock()
	}

	offset += 20
	friendCount, followerCount, followingCount := setupSidebarStatus(currentUser.ID)

	var data = map[string]interface{}{
		"Title":          "Conversation with " + user.Nickname + " (" + user.Username + ")",
		"Offset":         offset,
		"Pjax":           pjax,
		"Query":          query,
		"User":           user,
		"ConversationID": conversationID,
		"IsGroupChat":    false,
		"CurrentUser":    currentUser,
		"FriendCount":    friendCount,
		"FollowingCount": followingCount,
		"FollowerCount":  followerCount,
		"Messages":       messages,
		"MaxUploadSize":  settings.ImageHost.MaxUploadSize,
	}
	err = templates.ExecuteTemplate(w, "conversation.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Show the "Create Group Chat" page.
func showCreateGroupChat(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
	}
	pjax := r.Header.Get("X-PJAX") == ""
	offset, _ := strconv.Atoi(r.FormValue("offset"))

	friend_rows, err := db.Query("SELECT username, nickname, avatar, has_mh, online, hide_online, color, role, IFNULL(comment, '') FROM friendships LEFT JOIN users ON users.id = if(source = ?, target, source) LEFT JOIN profiles ON user = users.id WHERE (source = ? OR target = ?) AND (group_permissions = 0 OR (SELECT COUNT(*) FROM follows WHERE follow_to = ? AND follow_by = user) > 0) ORDER BY friendships.id DESC LIMIT 20 OFFSET ?", currentUser.ID, currentUser.ID, currentUser.ID, currentUser.ID, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var friends []user

	for friend_rows.Next() {
		var row user
		var role int

		friend_rows.Scan(&row.Username, &row.Nickname, &row.Avatar, &row.HasMii, &row.Online, &row.HideOnline, &row.Color, &role, &row.Comment)
		row.Avatar = getAvatar(row.Avatar, row.HasMii, 0)
		if role > 0 {
			row.Role.Image = getRoleImage(role)
		}

		friends = append(friends, row)
	}
	friend_rows.Close()

	friendCount, followerCount, followingCount := setupSidebarStatus(currentUser.ID)
	offset += 20

	var data = map[string]interface{}{
		"Title":          "Create Group Chat",
		"Pjax":           pjax,
		"Offset":         offset,
		"CurrentUser":    currentUser,
		"FriendCount":    friendCount,
		"FollowingCount": followingCount,
		"FollowerCount":  followerCount,
		"Friends":        friends,
		"Editing":        false,
	}
	err = templates.ExecuteTemplate(w, "create_group.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Show the "Edit Group Chat" page.
func showEditGroupChat(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
	}
	vars := mux.Vars(r)
	conversationID := vars["id"]
	pjax := r.Header.Get("X-PJAX") == ""
	offset, _ := strconv.Atoi(r.FormValue("offset"))

	member_rows, err := db.Query("SELECT username, nickname, avatar, has_mh, online, hide_online, color, role, IFNULL(comment, '') FROM friendships LEFT JOIN users ON users.id = if(source = ?, target, source) LEFT JOIN profiles ON profiles.user = users.id LEFT JOIN group_members ON users.id = group_members.user WHERE (source = ? or target = ?) AND (group_permissions = 0 OR (SELECT COUNT(*) FROM follows WHERE follow_to = ? AND follow_by = users.id) > 0) AND conversation = ? ORDER BY group_members.id ASC LIMIT 20 OFFSET ?", currentUser.ID, currentUser.ID, currentUser.ID, currentUser.ID, conversationID, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var members []user

	i := 1
	for member_rows.Next() {
		var row user
		var role int

		member_rows.Scan(&row.Username, &row.Nickname, &row.Avatar, &row.HasMii, &row.Online, &row.HideOnline, &row.Color, &role, &row.Comment)
		row.Avatar = getAvatar(row.Avatar, row.HasMii, 0)
		if role > 0 {
			row.Role.Image = getRoleImage(role)
		}
		row.Level = i

		members = append(members, row)
		i++
	}
	member_rows.Close()

	friend_rows, err := db.Query("SELECT username, nickname, avatar, has_mh, online, hide_online, color, role, IFNULL(comment, '') FROM friendships LEFT JOIN users ON users.id = if(source = ?, target, source) LEFT JOIN profiles ON user = users.id WHERE (source = ? or target = ?) AND (group_permissions = 0 OR (SELECT COUNT(*) FROM follows WHERE follow_to = ? AND follow_by = user) > 0) AND NOT EXISTS (SELECT user FROM group_members WHERE user = users.id AND conversation = ?) ORDER BY friendships.id DESC LIMIT 20 OFFSET ?", currentUser.ID, currentUser.ID, currentUser.ID, currentUser.ID, conversationID, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var friends []user

	for friend_rows.Next() {
		var row user
		var role int

		friend_rows.Scan(&row.Username, &row.Nickname, &row.Avatar, &row.HasMii, &row.Online, &row.HideOnline, &row.Color, &role, &row.Comment)
		row.Avatar = getAvatar(row.Avatar, row.HasMii, 0)
		if role > 0 {
			row.Role.Image = getRoleImage(role)
		}

		friends = append(friends, row)
	}
	friend_rows.Close()

	var byMe bool
	db.QueryRow("SELECT COUNT(*) FROM conversations WHERE id = ? AND source = ?", conversationID, currentUser.ID).Scan(&byMe)
	friendCount, followerCount, followingCount := setupSidebarStatus(currentUser.ID)
	offset += 20

	var data = map[string]interface{}{
		"Title":          "Edit Group Chat",
		"Pjax":           pjax,
		"Offset":         offset,
		"CurrentUser":    currentUser,
		"FriendCount":    friendCount,
		"FollowingCount": followingCount,
		"FollowerCount":  followerCount,
		"Members":        members,
		"Friends":        friends,
		"ConversationID": conversationID,
		"Editing":        true,
		"ByMe":           byMe,
	}
	err = templates.ExecuteTemplate(w, "create_group.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Show the FAQ page.
func showFAQPage(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}

	pjax := r.Header.Get("X-PJAX") == ""
	friendCount, followerCount, followingCount := setupSidebarStatus(currentUser.ID)

	var data = map[string]interface{}{
		"Title":          "Frequently Asked Questions (FAQ)",
		"Pjax":           pjax,
		"CurrentUser":    currentUser,
		"FriendCount":    friendCount,
		"FollowingCount": followingCount,
		"FollowerCount":  followerCount,
	}
	err := templates.ExecuteTemplate(w, "faq.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// Show a user's favorite communities.
func showFavorites(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}

	offset, _ := strconv.Atoi(r.FormValue("offset"))
	vars := mux.Vars(r)
	username := vars["username"]
	users := QueryUser(username, currentUser.Timezone)
	if len(users.Username) == 0 || checkIfBlocked(users.ID, currentUser.ID) {
		handle404(w, r)
		return
	}
	users.Avatar = getAvatar(users.Avatar, users.HasMii, 0)
	pjax := r.Header.Get("X-PJAX") == ""

	sidebar := setupProfileSidebar(users, currentUser, "favorites")

	favorite_rows, err := db.Query("SELECT communities.id, title, icon FROM communities LEFT JOIN community_favorites ON communities.id = community WHERE favorite_by = ? ORDER BY community_favorites.id DESC LIMIT 20 OFFSET ?", users.ID, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var favorites []user
	for favorite_rows.Next() {
		var row = user{}

		err = favorite_rows.Scan(&row.ID, &row.Nickname, &row.Avatar)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		favorites = append(favorites, row)
	}
	favorite_rows.Close()

	offset += 20

	var data = map[string]interface{}{
		"Title":       users.Nickname + "'s Favorite Communities",
		"Pjax":        pjax,
		"Offset":      offset,
		"CurrentUser": currentUser,
		"User":        users,
		"Sidebar":     sidebar,
		"Users":       favorites,
	}
	err = templates.ExecuteTemplate(w, "user_list.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Show a user's followers.
func showFollowers(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}

	offset, _ := strconv.Atoi(r.FormValue("offset"))
	vars := mux.Vars(r)
	username := vars["username"]
	users := QueryUser(username, currentUser.Timezone)
	if len(users.Username) == 0 || checkIfBlocked(users.ID, currentUser.ID) {
		handle404(w, r)
		return
	}
	users.Avatar = getAvatar(users.Avatar, users.HasMii, 0)
	pjax := r.Header.Get("X-PJAX") == ""
	sidebar := setupProfileSidebar(users, currentUser, "followers")

	follower_rows, _ := db.Query("SELECT username, nickname, avatar, has_mh, online, hide_online, color, role, IFNULL(comment, '') FROM follows LEFT JOIN users ON users.id = follow_by LEFT JOIN profiles ON user = users.id WHERE follow_to = ? ORDER BY follows.id DESC LIMIT 20 OFFSET ?", users.ID, offset)
	var followers []user

	for follower_rows.Next() {
		var row = user{}
		var role int

		err = follower_rows.Scan(&row.Username, &row.Nickname, &row.Avatar, &row.HasMii, &row.Online, &row.HideOnline, &row.Color, &role, &row.Comment)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		row.Avatar = getAvatar(row.Avatar, row.HasMii, 0)
		if role > 0 {
			row.Role.Image = getRoleImage(role)
		}

		followers = append(followers, row)
	}
	follower_rows.Close()

	offset += 20

	var data = map[string]interface{}{
		"Title":       users.Nickname + "'s Followers",
		"Pjax":        pjax,
		"Offset":      offset,
		"CurrentUser": currentUser,
		"User":        users,
		"Sidebar":     sidebar,
		"Users":       followers,
	}
	err := templates.ExecuteTemplate(w, "user_list.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Show who a user is following.
func showFollowing(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}

	offset, _ := strconv.Atoi(r.FormValue("offset"))
	vars := mux.Vars(r)
	username := vars["username"]
	users := QueryUser(username, currentUser.Timezone)
	if len(users.Username) == 0 || checkIfBlocked(users.ID, currentUser.ID) {
		handle404(w, r)
		return
	}
	users.Avatar = getAvatar(users.Avatar, users.HasMii, 0)
	pjax := r.Header.Get("X-PJAX") == ""

	sidebar := setupProfileSidebar(users, currentUser, "following")

	follower_rows, _ := db.Query("SELECT username, nickname, avatar, has_mh, online, hide_online, color, role, IFNULL(comment, '') FROM follows LEFT JOIN users ON users.id = follow_to LEFT JOIN profiles ON user = users.id WHERE follow_by = ? ORDER BY follows.id DESC LIMIT 20 OFFSET ?", users.ID, offset)
	var following []user

	for follower_rows.Next() {
		var row = user{}
		var role int

		err = follower_rows.Scan(&row.Username, &row.Nickname, &row.Avatar, &row.HasMii, &row.Online, &row.HideOnline, &row.Color, &role, &row.Comment)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		row.Avatar = getAvatar(row.Avatar, row.HasMii, 0)
		if role > 0 {
			row.Role.Image = getRoleImage(role)
		}

		following = append(following, row)
	}
	follower_rows.Close()

	offset += 20

	var data = map[string]interface{}{
		"Title":       "Users " + users.Nickname + " is Following",
		"Pjax":        pjax,
		"Offset":      offset,
		"CurrentUser": currentUser,
		"User":        users,
		"Sidebar":     sidebar,
		"Users":       following,
	}

	err := templates.ExecuteTemplate(w, "user_list.html", data)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Show a user's friend requests.
func showFriendRequests(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
	}

	var notify bool
	pjax := r.Header.Get("X-PJAX") == ""

	db.QueryRow("SELECT IF(COUNT(*) > 0, 1, 0) FROM notifications WHERE notif_to = ? AND merged IS NULL AND notif_read = 0", currentUser.ID).Scan(&notify)

	request_rows, err := db.Query("SELECT friend_requests.id, message, created_at, request_read, username, nickname, avatar, has_mh, online, hide_online, color, role FROM friend_requests INNER JOIN users ON users.id = request_by WHERE request_to = ? ORDER BY friend_requests.id DESC", &currentUser.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var requests []*friendRequest

	for request_rows.Next() {
		var row = &friendRequest{}
		var timestamp time.Time
		var role int

		err = request_rows.Scan(&row.ID, &row.Message, &timestamp, &row.Read, &row.ByUsername, &row.ByNickname, &row.ByAvatar, &row.ByHasMii, &row.ByOnline, &row.ByHideOnline, &row.ByColor, &role)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		row.ByAvatar = getAvatar(row.ByAvatar, row.ByHasMii, 0)
		if role > 0 {
			row.ByRoleImage, row.ByRoleOrganization = getRoleImageAndOrganization(role)
		}
		row.CreatedAt = timestamp.Format("01/02/2006 3:04 PM")
		row.CreatedAtUnix = timestamp.Unix()
		row.Date = humanTiming(timestamp, currentUser.Timezone)

		requests = append(requests, row)
	}
	request_rows.Close()

	stmt, _ := db.Prepare("UPDATE friend_requests SET request_read = 1 WHERE request_to = ?")
	stmt.Exec(&currentUser.ID)
	var msg wsMessage
	msg.Type = "notif"
	db.QueryRow("SELECT COUNT(*) FROM notifications WHERE notif_to = ? AND merged IS NULL AND notif_read = 0", currentUser.ID).Scan(&msg.Content)
	for client := range clients {
		//clients[client].Mutex.Lock()
		if clients[client].UserID == currentUser.ID {
			err := writeWs(clients[client], client, msg)
			if err != nil {
				client.Close()
				delete(clients, client)
			}
		}
		//clients[client].Mutex.Unlock()
	}

	friendCount, followerCount, followingCount := setupSidebarStatus(currentUser.ID)

	var data = map[string]interface{}{
		"Title":          "Friend Requests",
		"Pjax":           pjax,
		"CurrentUser":    currentUser,
		"Notify":         notify,
		"FriendCount":    friendCount,
		"FollowingCount": followingCount,
		"FollowerCount":  followerCount,
		"FriendRequests": requests,
	}
	err = templates.ExecuteTemplate(w, "friend_requests.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Show a user's friends.
func showFriends(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}

	offset, _ := strconv.Atoi(r.FormValue("offset"))
	vars := mux.Vars(r)
	username := vars["username"]
	users := QueryUser(username, currentUser.Timezone)
	if len(users.Username) == 0 || checkIfBlocked(users.ID, currentUser.ID) {
		handle404(w, r)
		return
	}
	users.Avatar = getAvatar(users.Avatar, users.HasMii, 0)
	pjax := r.Header.Get("X-PJAX") == ""

	sidebar := setupProfileSidebar(users, currentUser, "friends")

	friend_rows, _ := db.Query("SELECT username, nickname, avatar, has_mh, online, hide_online, color, role, IFNULL(comment, '') FROM friendships LEFT JOIN users ON users.id = if(source = ?, target, source) LEFT JOIN profiles ON user = users.id WHERE source = ? or target = ? ORDER BY friendships.id DESC LIMIT 20 OFFSET ?", users.ID, users.ID, users.ID, offset)
	var friends []user

	for friend_rows.Next() {
		var row = user{}
		var role int

		err = friend_rows.Scan(&row.Username, &row.Nickname, &row.Avatar, &row.HasMii, &row.Online, &row.HideOnline, &row.Color, &role, &row.Comment)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		row.Avatar = getAvatar(row.Avatar, row.HasMii, 0)
		if role > 0 {
			row.Role.Image = getRoleImage(role)
		}

		friends = append(friends, row)
	}
	friend_rows.Close()

	offset += 20

	var data = map[string]interface{}{
		"Title":       users.Nickname + "'s Friends",
		"Pjax":        pjax,
		"Offset":      offset,
		"CurrentUser": currentUser,
		"User":        users,
		"Sidebar":     sidebar,
		"Users":       friends,
	}
	err := templates.ExecuteTemplate(w, "user_list.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Show a group chat.
func showGroupChat(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
	}

	vars := mux.Vars(r)
	pjax := r.Header.Get("X-PJAX") == ""
	offset, _ := strconv.Atoi(r.FormValue("offset"))
	query := r.URL.Query().Get("q")
	id := vars["id"]

	var source int
	var target int
	err := db.QueryRow("SELECT source, target FROM conversations WHERE id = ?", id).Scan(&source, &target)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if source == 0 {
		handle404(w, r)
		return
	}
	if target != 0 {
		handle404(w, r)
		return
	}

	message_rows, err := db.Query("SELECT messages.id, created_at, created_by, feeling, body, image, attachment_type, url, url_type, post_type, username, avatar, has_mh, online, hide_online, color, role FROM messages LEFT JOIN users ON users.id = created_by WHERE conversation_id = ? AND is_rm = 0 AND body LIKE CONCAT('%', ?, '%') ORDER BY messages.id DESC LIMIT 20 OFFSET ?", id, query, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var messages []*message

	for message_rows.Next() {
		var row = &message{}
		var role int
		var timestamp time.Time
		var createdBy int

		err = message_rows.Scan(&row.ID, &timestamp, &createdBy, &row.Feeling, &row.BodyText, &row.Image, &row.AttachmentType, &row.URL, &row.URLType, &row.PostType, &row.ByUsername, &row.ByAvatar, &row.ByHasMii, &row.ByOnline, &row.ByHideOnline, &row.ByColor, &role)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		row.ByAvatar = getAvatar(row.ByAvatar, row.ByHasMii, row.Feeling)
		if role > 0 {
			row.ByRoleImage = getRoleImage(role)
		}

		row.Date = humanTiming(timestamp, currentUser.Timezone)
		row.DateUnix = timestamp.Unix()
		row.Body = parseBody(row.BodyText, false, true)

		if createdBy == currentUser.ID {
			row.ByMe = true
		}

		messages = append(messages, row)
	}
	message_rows.Close()

	var users []string
	user_rows, err := db.Query("SELECT nickname FROM group_members LEFT JOIN users ON user = users.id WHERE conversation = ? AND user != ? ORDER BY nickname ASC", id, currentUser.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for user_rows.Next() {
		var user string
		user_rows.Scan(&user)
		users = append(users, user)
	}
	user_rows.Close()
	title := getGroupName(users)

	_, err = db.Exec("UPDATE group_members SET unread_messages = 0 WHERE conversation = ? AND user = ?", id, currentUser.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var msg wsMessage
	msg.Type = "messageNotif"
	var unread int
	db.QueryRow("SELECT COUNT(*) FROM messages LEFT JOIN conversations ON conversation_id = conversations.id WHERE (source = ? OR target = ?) AND created_by <> ? AND msg_read = 0 AND messages.is_rm = 0 AND conversations.is_rm = 0", &currentUser.ID, &currentUser.ID, &currentUser.ID).Scan(&unread)
	var groupUnread int
	db.QueryRow("SELECT SUM(unread_messages) FROM group_members WHERE user = ?", currentUser.ID).Scan(&groupUnread)
	for client := range clients {
		//clients[client].Mutex.Lock()
		if clients[client].UserID == currentUser.ID {
			err := writeWs(clients[client], client, msg)
			if err != nil {
				client.Close()
				delete(clients, client)
			}
		}
		//clients[client].Mutex.Unlock()
	}

	offset += 20
	friendCount, followerCount, followingCount := setupSidebarStatus(currentUser.ID)

	var data = map[string]interface{}{
		"Title":          title,
		"Offset":         offset,
		"Pjax":           pjax,
		"Query":          query,
		"ConversationID": id,
		"IsGroupChat":    true,
		"CurrentUser":    currentUser,
		"FriendCount":    friendCount,
		"FollowingCount": followingCount,
		"FollowerCount":  followerCount,
		"Messages":       messages,
		"MaxUploadSize":  settings.ImageHost.MaxUploadSize,
	}
	err = templates.ExecuteTemplate(w, "conversation.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Show the legal information page.
func showLegalPage(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	pjax := r.Header.Get("X-PJAX") == ""

	friendCount, followerCount, followingCount := setupSidebarStatus(currentUser.ID)

	var data = map[string]interface{}{
		"Title":          "Legal Information",
		"Pjax":           pjax,
		"CurrentUser":    currentUser,
		"FriendCount":    friendCount,
		"FollowingCount": followingCount,
		"FollowerCount":  followerCount,
	}
	err := templates.ExecuteTemplate(w, "legal.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// Show a user's messages.
func showMessages(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
	}
	pjax := r.Header.Get("X-PJAX") == ""
	offset, _ := strconv.Atoi(r.FormValue("offset"))

	conversation_rows, err := db.Query("SELECT conversations.id, target, IFNULL(created_by, if(source = ?, target, source)), IFNULL(messages.created_at, conversations.created_at) sex, IFNULL(body, ''), IFNULL(image, ''), IFNULL(post_type, 0), IFNULL(msg_read, 1), IFNULL(username, conversations.id), IFNULL(nickname, ''), IFNULL(avatar, ''), IFNULL(has_mh, 0), IFNULL(online, 0), IFNULL(hide_online, 1), IFNULL(color, ''), IFNULL(role, 0) FROM conversations LEFT JOIN messages ON messages.id = (SELECT MAX(id) FROM messages WHERE messages.conversation_id = conversations.id AND is_rm = 0) LEFT JOIN users ON if(source = ?, target, source) = users.id LEFT JOIN group_members ON conversations.id = conversation WHERE (source = ? OR target = ? OR user = ?) AND conversations.is_rm = 0 GROUP BY conversations.id, messages.id, users.id ORDER BY sex DESC LIMIT 20 OFFSET ?", currentUser.ID, currentUser.ID, currentUser.ID, currentUser.ID, currentUser.ID, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var conversations []*conversation
	for conversation_rows.Next() {
		var row = &conversation{}
		var timestamp time.Time
		var role int

		err = conversation_rows.Scan(&row.ID, &row.Target, &row.CreatedBy, &timestamp, &row.BodyText, &row.Image, &row.PostType, &row.Read, &row.Username, &row.Nickname, &row.Icon, &row.HasMii, &row.Online, &row.HideOnline, &row.Color, &role)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if row.Target == 0 {
			row.Username = strconv.Itoa(row.ID)
			member_rows, err := db.Query("SELECT nickname FROM group_members LEFT JOIN users ON user = users.id WHERE conversation = ? AND user != ? ORDER BY nickname ASC", row.ID, currentUser.ID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			var members []string
			for member_rows.Next() {
				var member string
				member_rows.Scan(&member)
				members = append(members, member)
			}
			member_rows.Close()
			row.Nickname = getGroupName(members)

			err = db.QueryRow("SELECT IF(unread_messages > 0, 0, 1) FROM group_members WHERE conversation = ? AND user = ?", row.ID, currentUser.ID).Scan(&row.Read)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			if len(row.Icon) == 0 {
				db.QueryRow("SELECT avatar, has_mh, online, hide_online, role FROM users WHERE id = ?", row.CreatedBy).Scan(&row.Icon, &row.HasMii, &row.Online, &row.HideOnline, &role)
			} else {
				row.Color = ""
			}
		}
		row.Icon = getAvatar(row.Icon, row.HasMii, 0)
		if role > 0 {
			row.RoleImage = getRoleImage(role)
		}
		row.Date = humanTiming(timestamp, currentUser.Timezone)
		row.DateUnix = timestamp.Unix()

		conversations = append(conversations, row)
	}
	conversation_rows.Close()

	offset += 20
	friendCount, followerCount, followingCount := setupSidebarStatus(currentUser.ID)

	var data = map[string]interface{}{
		"Title":          "Messages",
		"Pjax":           pjax,
		"Offset":         offset,
		"CurrentUser":    currentUser,
		"FriendCount":    friendCount,
		"FollowingCount": followingCount,
		"FollowerCount":  followerCount,
		"Conversations":  conversations,
	}
	err = templates.ExecuteTemplate(w, "messages.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Show notifications.
func showNotifications(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
	}

	var notify bool
	pjax := r.Header.Get("X-PJAX") == ""

	db.QueryRow("SELECT IF(COUNT(*) > 0, 1, 0) FROM friend_requests WHERE request_to = ? AND request_read = 0", currentUser.ID).Scan(&notify)

	notif_rows, _ := db.Query("SELECT notifications.id, notif_type, notif_by, notif_post, notif_date, notif_read, username, nickname, avatar, has_mh, online, hide_online, color, role FROM notifications INNER JOIN users ON users.id = notifications.notif_by WHERE notif_to = ? AND merged IS NULL ORDER BY notif_date DESC LIMIT 50", currentUser.ID)
	var notifs []*notification

	for notif_rows.Next() {
		var row = &notification{}
		var timestamp time.Time
		var role int

		notif_rows.Scan(&row.ID, &row.Type, &row.By, &row.Post, &timestamp, &row.Read, &row.ByUsername, &row.ByNickname, &row.ByAvatar, &row.ByHasMii, &row.ByOnline, &row.ByHideOnline, &row.ByColor, &role)

		row.ByAvatar = getAvatar(row.ByAvatar, row.ByHasMii, 0)
		if role > 0 {
			row.ByRoleImage = getRoleImage(role)
		}

		row.Date = humanTiming(timestamp, currentUser.Timezone)
		row.DateUnix = timestamp.Unix()

		if row.Type == 0 || row.Type == 2 || row.Type == 3 || row.Type == 7 {
			db.QueryRow("SELECT body, post_type, is_rm | is_rm_by_admin FROM posts WHERE id = ?", row.Post).Scan(&row.PostText, &row.PostType, &row.PostIsRM)
		} else if row.Type == 1 {
			db.QueryRow("SELECT body, post_type, is_rm | is_rm_by_admin FROM comments WHERE id = ?", row.Post).Scan(&row.PostText, &row.PostType, &row.PostIsRM)
		}
		row.PostText = parsePreview(row.PostText, row.PostType, row.PostIsRM)

		db.QueryRow("SELECT COUNT(notif_by) FROM notifications WHERE merged = ? AND notif_by != ?", row.ID, row.By).Scan(&row.MergedCount)
		row.MergedOthers = row.MergedCount - 3

		if row.Type != 8 {
			merged_rows, err := db.Query("SELECT username, nickname, color FROM notifications INNER JOIN users ON users.id = notif_by WHERE merged = ? AND notif_by != ? GROUP BY notif_by, notif_date ORDER BY notif_date LIMIT 3", row.ID, row.By)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			var i int = 0
			for merged_rows.Next() {
				merged_rows.Scan(&row.MergedUsername[i], &row.MergedNickname[i], &row.MergedColor[i])
				i++
			}
			merged_rows.Close()
		}

		notifs = append(notifs, row)
	}
	notif_rows.Close()
	friendCount, followerCount, followingCount := setupSidebarStatus(currentUser.ID)

	stmt, _ := db.Prepare("UPDATE notifications SET notif_read = 1 WHERE notif_to = ?")
	stmt.Exec(&currentUser.ID)
	var msg wsMessage
	msg.Type = "notif"
	db.QueryRow("SELECT COUNT(*) FROM friend_requests WHERE request_to = ? AND request_read = 0", currentUser.ID).Scan(&msg.Content)
	for client := range clients {
		//clients[client].Mutex.Lock()
		if clients[client].UserID == currentUser.ID {
			err := writeWs(clients[client], client, msg)
			if err != nil {
				client.Close()
				delete(clients, client)
			}
		}
		//clients[client].Mutex.Unlock()
	}

	var data = map[string]interface{}{
		"Title":          "Notifications",
		"Pjax":           pjax,
		"CurrentUser":    currentUser,
		"Notify":         notify,
		"FriendCount":    friendCount,
		"FollowingCount": followingCount,
		"FollowerCount":  followerCount,
		"Notifs":         notifs,
	}
	err := templates.ExecuteTemplate(w, "notifications.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Show popular posts.
func showPopularPosts(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}

	vars := mux.Vars(r)
	community_id := vars["id"]
	communities := QueryCommunity(community_id, false)
	if len(communities.Title) == 0 {
		handle404(w, r)
		return
	}
	pjax := r.Header.Get("X-PJAX") == ""
	offset, _ := strconv.Atoi(r.FormValue("offset"))

	date := r.URL.Query().Get("date")
	if len(date) == 0 {
		date = time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	}
	dateParsed, err := time.Parse("2006-01-02", date)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var prevDate string
	if time.Now().Sub(dateParsed.AddDate(0, 0, 1)).Hours() >= 24 {
		prevDate = dateParsed.AddDate(0, 0, 1).Format("2006-01-02")
	}
	nextDate := dateParsed.AddDate(0, 0, -1).Format("2006-01-02")

	var favoriteGiven bool
	if len(currentUser.Username) > 0 {
		var favorited int
		err = db.QueryRow("SELECT COUNT(*) FROM community_favorites WHERE community = ? AND favorite_by = ?", &community_id, &currentUser.ID).Scan(&favorited)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if favorited > 0 {
			favoriteGiven = true
		}
	}

	post_rows, err := db.Query("SELECT posts.id, posts.created_by, posts.created_at, posts.edited_at, posts.feeling, posts.body, posts.image, posts.attachment_type, posts.is_spoiler, posts.post_type, posts.url, posts.url_type, posts.pinned, privacy, repost, username, nickname, avatar, has_mh, online, hide_online, color, role, (SELECT COUNT(*) FROM yeahs WHERE yeah_post = posts.id) + (SELECT COUNT(*) FROM comments WHERE post = posts.id AND is_rm = 0 AND is_rm_by_admin = 0) AS rating FROM posts INNER JOIN users ON users.id = created_by INNER JOIN yeahs ON yeah_post = posts.id LEFT JOIN comments ON post = comments.id WHERE community_id = ? AND cast(posts.created_at as date) = ? AND posts.is_rm = 0 AND posts.is_rm_by_admin = 0 AND migration = 0 AND (users.id NOT IN (SELECT if(source = ?, target, source) FROM blocks WHERE (source = ? AND target = users.id) OR (source = users.id AND target = ?)) OR ? > 0) AND IF(posts.created_by = ?, true, LOWER(posts.body) NOT REGEXP LOWER(?)) AND (privacy = 0 OR (privacy IN (1, 2, 3, 4) AND (SELECT COUNT(*) FROM friendships WHERE source = ? AND target = posts.created_by OR source = posts.created_by AND target = ? LIMIT 1) = 1) OR (privacy IN (1, 3, 5, 6) AND (SELECT COUNT(*) FROM follows WHERE follow_to = posts.created_by AND follow_by = ? LIMIT 1) = 1) OR (privacy IN (1, 2, 5, 7) AND (SELECT COUNT(*) FROM follows WHERE follow_to = ? AND follow_by = posts.created_by) = 1) OR (privacy = 8 AND ? > 0) OR posts.created_by = ?) GROUP BY posts.id ORDER BY rating DESC LIMIT 25 OFFSET ?", community_id, dateParsed, currentUser.ID, currentUser.ID, currentUser.ID, currentUser.Level, currentUser.ID, escapeForbiddenKeywords(currentUser.ForbiddenKeywords), currentUser.ID, currentUser.ID, currentUser.ID, currentUser.ID, currentUser.Level, currentUser.ID, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var posts []*post

	for post_rows.Next() {
		var row = &post{}
		var rating int

		err = post_rows.Scan(&row.ID, &row.CreatedBy, &row.CreatedAtTime, &row.EditedAtTime, &row.Feeling, &row.BodyText, &row.Image, &row.AttachmentType, &row.IsSpoiler, &row.PostType, &row.URL, &row.URLType, &row.Pinned, &row.Privacy, &row.RepostID, &row.PosterUsername, &row.PosterNickname, &row.PosterIcon, &row.PosterHasMii, &row.PosterOnline, &row.PosterHideOnline, &row.PosterColor, &row.PosterRoleID, &rating)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		row = setupPost(row, currentUser, 0, 0)
		posts = append(posts, row)
	}
	post_rows.Close()

	offset += 25

	var data = map[string]interface{}{
		"Title":         communities.Title,
		"Pjax":          pjax,
		"Offset":        offset,
		"CurrentUser":   currentUser,
		"Community":     communities,
		"FavoriteGiven": favoriteGiven,
		"PopularPosts":  true,
		"PrevDate":      prevDate,
		"CurrentDate":   date,
		"NextDate":      nextDate,
		"Posts":         posts,
	}
	err = templates.ExecuteTemplate(w, "communities.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	return
}

// Show a post.
func showPost(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}

	vars := mux.Vars(r)
	post_id := vars["id"]
	pjax := r.Header.Get("X-PJAX") == ""

	var posts = post{}
	db.QueryRow("SELECT posts.id, created_by, community_id, created_at, edited_at, feeling, body, image, attachment_type, is_spoiler, url, url_type, pinned, privacy, repost, post_type, migration, migrated_id, migrated_community, is_rm_by_admin, username, nickname, avatar, has_mh, online, hide_online, color, role FROM posts LEFT JOIN users ON users.id = created_by WHERE posts.id = ? AND is_rm = 0 AND (privacy = 0 OR (privacy IN (1, 2, 3, 4) AND (SELECT COUNT(*) FROM friendships WHERE source = ? AND target = created_by OR source = created_by AND target = ? LIMIT 1) = 1) OR (privacy IN (1, 3, 5, 6) AND (SELECT COUNT(*) FROM follows WHERE follow_to = created_by AND follow_by = ? LIMIT 1) = 1) OR (privacy IN (1, 2, 5, 7) AND (SELECT COUNT(*) FROM follows WHERE follow_to = ? AND follow_by = created_by) = 1) OR (privacy = 8 AND ? > 0) OR created_by = ?)", post_id, currentUser.ID, currentUser.ID, currentUser.ID, currentUser.ID, currentUser.Level, currentUser.ID).Scan(&posts.ID, &posts.CreatedBy, &posts.CommunityID, &posts.CreatedAtTime, &posts.EditedAtTime, &posts.Feeling, &posts.BodyText, &posts.Image, &posts.AttachmentType, &posts.IsSpoiler, &posts.URL, &posts.URLType, &posts.Pinned, &posts.Privacy, &posts.RepostID, &posts.PostType, &posts.MigrationID, &posts.MigratedID, &posts.MigratedCommunity, &posts.IsRMByAdmin, &posts.PosterUsername, &posts.PosterNickname, &posts.PosterIcon, &posts.PosterHasMii, &posts.PosterOnline, &posts.PosterHideOnline, &posts.PosterColor, &posts.PosterRoleID)
	if len(posts.PosterUsername) == 0 {
		handle404(w, r)
		return
	}

	community := QueryCommunity(strconv.Itoa(posts.CommunityID), true) // todo: get rid of this

	posts.PosterIcon = getAvatar(posts.PosterIcon, posts.PosterHasMii, posts.Feeling)
	if posts.PosterRoleID > 0 {
		posts.PosterRoleImage, posts.PosterRoleOrganization = getRoleImageAndOrganization(posts.PosterRoleID)
	}

	posts.CreatedAt = humanTiming(posts.CreatedAtTime, currentUser.Timezone)
	posts.CreatedAtUnix = posts.CreatedAtTime.Unix()
	if posts.EditedAtTime.Sub(posts.CreatedAtTime).Minutes() > 5 {
		posts.EditedAt = humanTiming(posts.EditedAtTime, currentUser.Timezone)
		posts.EditedAtUnix = posts.EditedAtTime.Unix()
	}
	if len(posts.MigratedID) == 0 || strings.Contains(posts.BodyText, ":markdown:") {
		posts.Body = parseBodyWithLineBreaks(posts.BodyText, false, true)
	} else {
		posts.Body = parseBodyWithLineBreaks(posts.BodyText, false, false)
	}
	if posts.RepostID > 0 {
		var repost post
		db.QueryRow("SELECT posts.id, created_by, created_at, edited_at, feeling, body, image, attachment_type, is_spoiler, post_type, url, url_type, pinned, privacy, repost, migration, migrated_id, migrated_community, is_rm_by_admin, communities.id, title, icon, rm, username, nickname, avatar, has_mh, online, hide_online, color, role FROM posts LEFT JOIN communities ON communities.id = community_id LEFT JOIN users ON users.id = created_by WHERE posts.id = ? AND is_rm = 0 AND users.id NOT IN (SELECT if(source = ?, target, source) FROM blocks WHERE (source = ? AND target = users.id) OR (source = users.id AND target = ?)) AND IF(created_by = ?, true, LOWER(body) NOT REGEXP LOWER(?)) AND (privacy = 0 OR (privacy IN (1, 2, 3, 4) AND (SELECT COUNT(*) FROM friendships WHERE source = ? AND target = created_by OR source = created_by AND target = ? LIMIT 1) = 1) OR (privacy IN (1, 3, 5, 6) AND (SELECT COUNT(*) FROM follows WHERE follow_to = created_by AND follow_by = ? LIMIT 1) = 1) OR (privacy IN (1, 2, 5, 7) AND (SELECT COUNT(*) FROM follows WHERE follow_to = ? AND follow_by = created_by) = 1) OR (privacy = 8 AND ? > 0) OR created_by = ?) LIMIT 1", posts.RepostID, currentUser.ID, currentUser.ID, currentUser.ID, currentUser.ID, escapeForbiddenKeywords(currentUser.ForbiddenKeywords), currentUser.ID, currentUser.ID, currentUser.ID, currentUser.ID, currentUser.Level, currentUser.ID).Scan(&repost.ID, &repost.CreatedBy, &repost.CreatedAtTime, &repost.EditedAtTime, &repost.Feeling, &repost.BodyText, &repost.Image, &repost.AttachmentType, &repost.IsSpoiler, &repost.PostType, &repost.URL, &repost.URLType, &repost.Pinned, &repost.Privacy, &repost.RepostID, &repost.MigrationID, &repost.MigratedID, &repost.MigratedCommunity, &repost.IsRMByAdmin, &repost.CommunityID, &repost.CommunityName, &repost.CommunityIcon, &repost.CommunityRM, &repost.PosterUsername, &repost.PosterNickname, &repost.PosterIcon, &repost.PosterHasMii, &repost.PosterOnline, &repost.PosterHideOnline, &repost.PosterColor, &repost.PosterRoleID)
		posts.Repost = &repost
		posts.Repost.Type = 3
		if len(posts.Repost.CommunityName) > 0 {
			posts.Repost = setupPost(posts.Repost, currentUser, 3, 0)
		}
	}
	if posts.PostType == 2 {
		posts.Poll = getPoll(posts.ID, currentUser.ID)
	}
	if posts.MigrationID > 0 {
		posts.MigrationImage, posts.MigrationURL, community.Title, community.Icon = getPostMigration(posts.MigrationID, posts.MigratedCommunity)
	}
	posts.CanYeah = checkIfCanYeah(currentUser, posts.CreatedBy)

	var favoritePost string
	isFavorite := false
	db.QueryRow("SELECT favorite FROM profiles WHERE user = ?", currentUser.ID).Scan(&favoritePost)
	if favoritePost == post_id {
		isFavorite = true
	}

	var yeahs []yeah
	var pinnedComments []comment
	var comments []comment
	if !posts.IsRMByAdmin {
		if len(currentUser.Username) > 0 {
			db.QueryRow("SELECT COUNT(*) FROM yeahs WHERE yeah_post = ? AND yeah_by = ? AND on_comment = 0 LIMIT 1", posts.ID, currentUser.ID).Scan(&posts.Yeahed)
		}
		db.QueryRow("SELECT COUNT(*) FROM yeahs WHERE yeah_post = ? AND on_comment=0", post_id).Scan(&posts.YeahCount)
		db.QueryRow("SELECT COUNT(*) FROM comments WHERE post = ? AND is_rm = 0", post_id).Scan(&posts.CommentCount)

		yeah_rows, _ := db.Query("SELECT yeahs.id, username, avatar, has_mh, role FROM yeahs LEFT JOIN users ON users.id = yeah_by WHERE yeah_post = ? AND yeah_by != ? AND on_comment=0 ORDER BY yeahs.id DESC", post_id, currentUser.ID)

		for yeah_rows.Next() {
			var row = yeah{}
			var role int

			err = yeah_rows.Scan(&row.ID, &row.Username, &row.Avatar, &row.HasMii, &role)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			row.Avatar = getAvatar(row.Avatar, row.HasMii, posts.Feeling)
			if role > 0 {
				row.Role = getRoleImage(role)
			}

			yeahs = append(yeahs, row)
		}
		yeah_rows.Close()

		offset := posts.CommentCount - 20
		if offset < 0 {
			offset = 0
		}
		comment_rows, _ := db.Query("SELECT comments.id, created_by, created_at, edited_at, feeling, body, image, attachment_type, is_spoiler, post_type, url, url_type, pinned, is_rm_by_admin, username, nickname, avatar, has_mh, online, hide_online, color, role FROM comments LEFT JOIN users ON users.id = created_by WHERE post = ? AND is_rm = 0 AND (users.id NOT IN (SELECT if(source = ?, target, source) FROM blocks WHERE (source = ? AND target = users.id) OR (source = users.id AND target = ?)) OR ? > 0) AND IF(created_by = ?, true, LOWER(body) NOT REGEXP LOWER(?)) ORDER BY created_at ASC LIMIT 20 OFFSET ?", post_id, currentUser.ID, currentUser.ID, currentUser.ID, currentUser.Level, currentUser.ID, escapeForbiddenKeywords(currentUser.ForbiddenKeywords), offset)
		for comment_rows.Next() {
			var row = comment{}
			var timestamp time.Time
			var editedAt time.Time
			var role int

			err := comment_rows.Scan(&row.ID, &row.CreatedBy, &timestamp, &editedAt, &row.Feeling, &row.BodyText, &row.Image, &row.AttachmentType, &row.IsSpoiler, &row.PostType, &row.URL, &row.URLType, &row.Pinned, &row.IsRMByAdmin, &row.CommenterUsername, &row.CommenterNickname, &row.CommenterIcon, &row.CommenterHasMii, &row.CommenterOnline, &row.CommenterHideOnline, &row.CommenterColor, &role)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			row.CommenterIcon = getAvatar(row.CommenterIcon, row.CommenterHasMii, row.Feeling)

			if role > 0 {
				row.CommenterRoleImage = getRoleImage(role)
			}

			row.CreatedAt = humanTiming(timestamp, currentUser.Timezone)
			row.CreatedAtUnix = timestamp.Unix()
			if editedAt.Sub(timestamp).Minutes() > 5 {
				row.EditedAt = humanTiming(editedAt, currentUser.Timezone)
				row.EditedAtUnix = editedAt.Unix()
			}
			row.Body = parseBody(row.BodyText, false, true)

			row.ByMe = row.CreatedBy == posts.CreatedBy
			row.ByMii = row.CreatedBy == currentUser.ID
			row.CanYeah = checkIfCanYeah(currentUser, row.CreatedBy)

			db.QueryRow("SELECT 1 FROM yeahs WHERE yeah_post = ? AND yeah_by = ? AND on_comment = 1", row.ID, currentUser.ID).Scan(&row.Yeahed)
			db.QueryRow("SELECT COUNT(*) FROM yeahs WHERE yeah_post = ? AND on_comment = 1", row.ID).Scan(&row.YeahCount)

			if row.Pinned {
				pinnedComments = append(pinnedComments, row)
			} else {
				comments = append(comments, row)
			}
		}
		comment_rows.Close()
	}

	isBlocked := false
	if checkIfEitherBlocked(posts.CreatedBy, currentUser.ID) {
		isBlocked = true
	}

	var data = map[string]interface{}{
		"Title":          posts.PosterNickname + "'s Post",
		"Pjax":           pjax,
		"CurrentUser":    currentUser,
		"Community":      community,
		"Post":           posts,
		"Yeahs":          yeahs,
		"Reasons":        settings.ReportReasons,
		"PinnedComments": pinnedComments,
		"Comments":       comments,
		"IsFavorite":     isFavorite,
		"IsBlocked":      isBlocked,
		"MaxUploadSize":  settings.ImageHost.MaxUploadSize,
	}
	err := templates.ExecuteTemplate(w, "post.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Show a user's profile settings.
func showProfileSettings(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	pjax := r.Header.Get("X-PJAX") == ""
	sidebar := setupProfileSidebar(currentUser, currentUser, "settings")

	migration_rows, err := db.Query("SELECT id, image, password_required FROM migrations WHERE is_rm = 0")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var migrations []migrationOption
	for migration_rows.Next() {
		var row migrationOption
		migration_rows.Scan(&row.ID, &row.Image, &row.PasswordRequired)
		migrations = append(migrations, row)
	}

	import_rows, err := db.Query("SELECT imports.id, image, username FROM imports LEFT JOIN migrations ON migration = migrations.id WHERE user = ? ORDER BY id DESC", currentUser.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var imports []importLog
	for import_rows.Next() {
		var row importLog
		import_rows.Scan(&row.ID, &row.Image, &row.Username)
		imports = append(imports, row)
	}
	import_rows.Close()

	var data = map[string]interface{}{
		"Title":       "Profile Settings",
		"Pjax":        pjax,
		"User":        currentUser,
		"CurrentUser": currentUser,
		"Profile":     sidebar.Profile,
		"Sidebar":     sidebar,
		"Migrations":  migrations,
		"Imports":     imports,
	}

	err = templates.ExecuteTemplate(w, "profile_settings.html", data)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Show the signup page.
func signup(w http.ResponseWriter, r *http.Request) {
	session := sessions.Start(w, r)
	user_id, _ := session.GetInt("user_id")
	if !settings.AllowSignups || user_id > 0 {
		if settings.ForceLogins {
			http.Redirect(w, r, "/login", 302)
		} else {
			http.Redirect(w, r, "/", 302)
		}
		return
	}
	if r.Method != "POST" {
		var currentUser user
		currentUser.CSRFToken = csrf.Token(r)
		var data = map[string]interface{}{
			"Title":       "Sign Up",
			"CurrentUser": currentUser,
			"ReCAPTCHA":   settings.ReCAPTCHA,
		}
		err := templates.ExecuteTemplate(w, "signup.html", data)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Define user registration info.
	username := r.FormValue("username")
	nickname := r.FormValue("nickname")
	avatar := r.FormValue("image")
	avatar_id := r.FormValue("image")
	mh := r.FormValue("mh")
	has_mh := false
	if len(mh) > 0 && len(avatar) == 0 {
		has_mh = true
	}
	email := r.FormValue("email")
	nnid := r.FormValue("nnid")
	password := r.FormValue("password")
	confirm := r.FormValue("confirm")
	ip := getIP(r)
	ipHost, _, _ := net.SplitHostPort(ip)
	level := "0"
	role := "0"
	last_seen := time.Now()
	color := ""
	yeah_notifications := "1"

	users := QueryUser(username, getTimezone(ip))
	ip, _, _ = net.SplitHostPort(ip)

	if len(users.Nickname) == 0 {
		username_check, err := regexp.MatchString("^[A-Za-z0-9-._]{4,32}$", username)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !username_check {
			http.Error(w, "Your username is invalid.", http.StatusBadRequest)
			return
		}
		if utf8.RuneCountInString(email) > 255 {
			http.Error(w, "Your email is too long.", http.StatusBadRequest)
			return
		}
		if password != confirm {
			http.Error(w, "Your password and confirm password must match.", http.StatusBadRequest)
			return
		}
		if len(password) == 0 {
			http.Error(w, "You must enter a password.", http.StatusBadRequest)
			return
		}
		if len(nickname) == 0 {
			http.Error(w, "You must enter a nickname.", http.StatusBadRequest)
			return
		}

		if len(email) > 0 {
			var emailCount int
			db.QueryRow("SELECT COUNT(*) FROM users WHERE email = ?", email).Scan(&emailCount)
			if emailCount > 0 {
				http.Error(w, "A user already exists with that email.", http.StatusBadRequest)
				return
			}
			err := checkmail.ValidateFormat(email)
			if err != nil {
				http.Error(w, fmt.Sprintf("Email error: %s", err.Error()), http.StatusBadRequest)
				return
			}
		}

		if len(avatar) > 0 {
			imageURL := ""
			db.QueryRow("SELECT value FROM images WHERE id = ?", avatar).Scan(&imageURL)
			if len(imageURL) == 0 {
				http.Error(w, "Invalid image.", http.StatusBadRequest)
				return
			}
			avatar = imageURL
		}

		if len(nnid) > 0 {
			nnidCheck, _ := regexp.MatchString("^[A-Za-z0-9-._]{6,16}$", nnid)
			if !nnidCheck {
				http.Error(w, "Your Nintendo Network ID is invalid.", http.StatusBadRequest)
				return
			}
		}

		if len(avatar) == 0 && has_mh {
			avatar = mh
		}

		if settings.ReCAPTCHA.Enabled {
			data := url.Values{}
			data.Set("secret", settings.ReCAPTCHA.SecretKey)
			data.Set("response", r.FormValue("g-recaptcha-response"))
			data.Set("remoteip", ipHost)
			resp, err := http.Post("https://www.google.com/recaptcha/api/siteverify", "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			defer resp.Body.Close()
			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			jsonBody := make(map[string]interface{})
			json.Unmarshal(body, &jsonBody)
			if jsonBody["success"] == 0 {
				http.Error(w, "The reCAPTCHA was not solved correctly.", http.StatusBadRequest)
				return
			}
		}
		if len(settings.IPHubKey) > 0 {
			client := &http.Client{}
			req, _ := http.NewRequest("GET", "https://v2.api.iphub.info/ip/"+ipHost, nil)
			req.Header.Set("X-Key", settings.IPHubKey)
			res, _ := client.Do(req)
			defer res.Body.Close()
			body, err := ioutil.ReadAll(res.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			jsonBody := make(map[string]interface{})
			json.Unmarshal(body, &jsonBody)
			if jsonBody["block"] == 1 {
				http.Error(w, "You cannot sign up using a proxy.", http.StatusBadRequest)
				return
			}
		}

		// Hash the password using bcrypt.
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)

		if len(hashedPassword) != 0 && err == nil {
			// Prepare the statement.
			stmt, err := db.Prepare("INSERT users SET username = ?, nickname = ?, avatar = ?, has_mh = ?, email = ?, password = ?, ip = ?, level = ?, role = ?, last_seen = ?, color = ?, yeah_notifications = ?, forbidden_keywords = ''")
			if err == nil {
				// If there's no errors, we can go ahead and execute the statement.
				_, err := stmt.Exec(&username, &nickname, &avatar, &has_mh, &email, &hashedPassword, &ipHost, &level, &role, &last_seen, &color, &yeah_notifications)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				users = QueryUser(username, getTimezone(getIP(r)))

				user := users.ID
				created_at := time.Now()
				gender := 0
				region := ""

				stmt, err := db.Prepare("INSERT profiles SET user = ?, created_at = ?, nnid = ?, mh = ?, gender = ?, region = ?, comment = '', nnid_visibility = 0, yeah_visibility = 0, reply_visibility = 1, discord = '', steam = '', psn = '', switch_code = '', twitter = '', youtube = '', avatar_image = ?, avatar_id = ?")
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				_, err = stmt.Exec(user, created_at, nnid, mh, gender, region, avatar, avatar_id)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}

				session := sessions.Start(w, r)
				session.Set("username", users.Username)
				session.Set("user_id", users.ID)

				if settings.Webhooks.Enabled && len(settings.Webhooks.Signups) > 0 {
					if username != nickname {
						nickname += " (" + username + ")"
					}
					if len(email) == 0 {
						email = "None"
					} else {
						email = "`" + escapeMarkdown(email) + "`"
					}
					data := map[string]interface{}{
						"content": fmt.Sprintf("%s just signed up from the IP %s.\nEmail address: %s\nUser agent: %s\nURL: %s", escapeMarkdown(nickname), ipHost, email, escapeMarkdown(r.UserAgent()), getHostname(r.Host)+"/users/"+url.PathEscape(username)),
					}
					jsonBytes, err := json.Marshal(data)
					if err != nil {
						fmt.Println(err.Error())
					}
					_, err = http.Post(settings.Webhooks.Signups, "application/json", bytes.NewBuffer(jsonBytes))
					if err != nil {
						fmt.Println(err.Error())
					}
				}

				http.Redirect(w, r, "/", 302)
			}
		} else {
			http.Redirect(w, r, "/signup", 302)
		}
	} else {
		http.Error(w, "That user already exists.", http.StatusBadRequest)
	}
}

// Show a user's recent communities.
func showRecentCommunities(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
	}
	offset, _ := strconv.Atoi(r.FormValue("offset"))

	community_rows, err := db.Query("SELECT id, title, icon, id IN (SELECT DISTINCT community_id FROM posts WHERE created_by = ? AND is_rm = 0 AND migration = 0) AS recent FROM communities WHERE (rm = 0 OR id = 0) AND permissions <= ? ORDER BY recent DESC, id DESC LIMIT 20 OFFSET ?", currentUser.ID, currentUser.Level, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var communities []community
	for community_rows.Next() {
		var row community
		var recent bool
		err = community_rows.Scan(&row.ID, &row.Title, &row.Icon, &recent)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		communities = append(communities, row)
	}
	community_rows.Close()

	offset += 20

	var data = map[string]interface{}{
		"Offset":      offset,
		"Communities": communities,
	}
	err = templates.ExecuteTemplate(w, "recent_communities.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Show the "Reset Password" page.
func showResetPassword(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}

	var data map[string]interface{}
	if r.Method == "POST" {
		email := r.FormValue("email")
		var userID int
		var username string
		db.QueryRow("SELECT id, username FROM users WHERE email = ? ORDER BY id DESC LIMIT 1", email).Scan(&userID, &username)
		if len(username) == 0 {
			data = map[string]interface{}{
				"Title":       "Reset Password",
				"CurrentUser": currentUser,
				"Action":      "error",
				"Error":       "No user was found with that email address.",
			}
		} else {
			token := generateLoginToken()
			stmt, _ := db.Prepare("INSERT INTO password_resets (token, user) VALUES (?, ?)")
			stmt.Exec(token, userID)

			auth := smtp.PlainAuth("", settings.SMTP.Email, settings.SMTP.Password, settings.SMTP.Hostname)

			hostname := getHostname(r.Host)
			message := fmt.Sprintf("Subject: Password reset for %s\nFrom: psy gangnam style hd download shit ass little fucking penis <%s>\nContent-Type: text/html\n\n<!DOCTYPE html><html><body><img src=\"%s/assets/img/menu-logo.png\"><br>A password reset request has been made for your account.<br>If you initiated this request, go here: <a href=\"%s/reset?token=%s\">%s/reset?token=%s</a><br>Otherwise, you can probably ignore this email, as these kinds of requests can be sent by anyone.</body></html>", username, settings.SMTP.Email, hostname, hostname, token, hostname, token)
			err = smtp.SendMail(settings.SMTP.Hostname+settings.SMTP.Port, auth, settings.SMTP.Email, []string{email}, []byte(message))
			if err != nil {
				data = map[string]interface{}{
					"Title":       "Reset Password",
					"CurrentUser": currentUser,
					"Action":      "error",
					"Error":       err.Error(),
				}
			} else {
				data = map[string]interface{}{
					"Title":       "Reset Password",
					"CurrentUser": currentUser,
					"Action":      "sent",
					"Email":       email,
				}
			}
		}
	} else if settings.SMTP.Enabled {
		data = map[string]interface{}{
			"Title":       "Reset Password",
			"CurrentUser": currentUser,
			"Action":      "request",
			"CSRFField":   csrf.TemplateField(r),
		}
	} else {
		data = map[string]interface{}{
			"Title":       "Reset Password",
			"CurrentUser": currentUser,
			"Action":      "disabled",
		}
	}

	err = templates.ExecuteTemplate(w, "reset.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Show the rules page.
func showRulesPage(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	pjax := r.Header.Get("X-PJAX") == ""

	var friendCount int
	var followingCount int
	var followerCount int

	db.QueryRow("SELECT COUNT(*) FROM friendships WHERE source = ? OR target = ?", currentUser.ID, currentUser.ID).Scan(&friendCount)
	db.QueryRow("SELECT COUNT(*) FROM follows WHERE follow_by = ?", currentUser.ID).Scan(&followingCount)
	db.QueryRow("SELECT COUNT(*) FROM follows WHERE follow_to = ?", currentUser.ID).Scan(&followerCount)

	var data = map[string]interface{}{
		"Title":          "Indigo Rules",
		"Pjax":           pjax,
		"CurrentUser":    currentUser,
		"FriendCount":    friendCount,
		"FollowingCount": followingCount,
		"FollowerCount":  followerCount,
	}
	err := templates.ExecuteTemplate(w, "rules.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// Show a user page.
func showUser(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}

	vars := mux.Vars(r)
	username := vars["username"]
	user := QueryUser(username, currentUser.Timezone)
	if len(user.Username) == 0 || checkIfBlocked(user.ID, currentUser.ID) {
		handle404(w, r)
		return
	}
	userAvatarBackup := user.Avatar
	user.Avatar = getAvatar(user.Avatar, user.HasMii, 0)
	pjax := r.Header.Get("X-PJAX") == ""
	sidebar := setupProfileSidebar(user, currentUser, "main")

	post_rows, err := db.Query("SELECT posts.id, community_id, created_at, edited_at, feeling, body, image, attachment_type, is_spoiler, post_type, url, url_type, pinned, privacy, repost, migration, migrated_id, migrated_community, is_rm_by_admin, title, icon, rm FROM posts LEFT JOIN communities ON communities.id = community_id WHERE created_by = ? AND is_rm = 0 AND IF(created_by = ?, true, LOWER(body) NOT REGEXP LOWER(?)) AND (privacy = 0 OR (privacy IN (1, 2, 3, 4) AND (SELECT COUNT(*) FROM friendships WHERE source = ? AND target = created_by OR source = created_by AND target = ? LIMIT 1) = 1) OR (privacy IN (1, 3, 5, 6) AND (SELECT COUNT(*) FROM follows WHERE follow_to = created_by AND follow_by = ? LIMIT 1) = 1) OR (privacy IN (1, 2, 5, 7) AND (SELECT COUNT(*) FROM follows WHERE follow_to = ? AND follow_by = created_by) = 1) OR (privacy = 8 AND ? > 0) OR created_by = ?) ORDER BY created_at DESC, posts.id DESC LIMIT 3", user.ID, currentUser.ID, escapeForbiddenKeywords(currentUser.ForbiddenKeywords), currentUser.ID, currentUser.ID, currentUser.ID, currentUser.ID, currentUser.Level, currentUser.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var posts []*post
	for post_rows.Next() {
		var row = &post{}

		post_rows.Scan(&row.ID, &row.CommunityID, &row.CreatedAtTime, &row.EditedAtTime, &row.Feeling, &row.BodyText, &row.Image, &row.AttachmentType, &row.IsSpoiler, &row.PostType, &row.URL, &row.URLType, &row.Pinned, &row.Privacy, &row.RepostID, &row.MigrationID, &row.MigratedID, &row.MigratedCommunity, &row.IsRMByAdmin, &row.CommunityName, &row.CommunityIcon, &row.CommunityRM)
		row.CreatedBy = user.ID
		row.PosterUsername = user.Username
		row.PosterNickname = user.Nickname
		row.PosterIcon = userAvatarBackup
		row.PosterHasMii = user.HasMii
		row.PosterOnline = user.Online
		row.PosterHideOnline = user.HideOnline
		row.PosterColor = user.Color
		row.PosterRoleImage = user.Role.Image
		row = setupPost(row, currentUser, -1, 2)
		posts = append(posts, row)
	}
	post_rows.Close()

	yeah_rows, err := db.Query("SELECT posts.id, created_by, community_id, created_at, edited_at, feeling, body, image, attachment_type, is_spoiler, post_type, url, url_type, pinned, privacy, repost, migration, migrated_id, migrated_community, username, nickname, avatar, has_mh, online, hide_online, color, role, title, icon, rm FROM yeahs INNER JOIN posts ON posts.id = yeah_post INNER JOIN users ON users.id = posts.created_by INNER JOIN communities ON communities.id = community_id WHERE yeah_by = ? AND on_comment = 0 AND is_rm = 0 AND is_rm_by_admin = 0 AND users.id NOT IN (SELECT if(source = ?, target, source) FROM blocks WHERE (source = ? AND target = users.id) OR (source = users.id AND target = ?)) AND IF(created_by = ?, true, LOWER(body) NOT REGEXP LOWER(?)) ORDER BY created_at DESC LIMIT 3", user.ID, currentUser.ID, currentUser.ID, currentUser.ID, currentUser.ID, escapeForbiddenKeywords(currentUser.ForbiddenKeywords))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var yeahs []*post
	for yeah_rows.Next() {
		var row = &post{}

		yeah_rows.Scan(&row.ID, &row.CreatedBy, &row.CommunityID, &row.CreatedAtTime, &row.EditedAtTime, &row.Feeling, &row.BodyText, &row.Image, &row.AttachmentType, &row.IsSpoiler, &row.PostType, &row.URL, &row.URLType, &row.Pinned, &row.Privacy, &row.RepostID, &row.MigrationID, &row.MigratedID, &row.MigratedCommunity, &row.PosterUsername, &row.PosterNickname, &row.PosterIcon, &row.PosterHasMii, &row.PosterOnline, &row.PosterHideOnline, &row.PosterColor, &row.PosterRoleID, &row.CommunityName, &row.CommunityIcon, &row.CommunityRM)
		row = setupPost(row, currentUser, -1, 0)
		yeahs = append(yeahs, row)
	}
	yeah_rows.Close()

	var data = map[string]interface{}{
		"Title":       user.Nickname + "'s Profile",
		"Pjax":        pjax,
		"CurrentUser": currentUser,
		"User":        user,
		"Sidebar":     sidebar,
		"Posts":       posts,
		"Yeahs":       yeahs,
	}
	err = templates.ExecuteTemplate(w, "user.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Show user comments
func showUserComments(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}

	vars := mux.Vars(r)
	username := vars["username"]
	user := QueryUser(username, currentUser.Timezone)
	if len(user.Username) == 0 || checkIfBlocked(user.ID, currentUser.ID) {
		handle404(w, r)
		return
	}
	userAvatarBackup := user.Avatar
	user.Avatar = getAvatar(user.Avatar, user.HasMii, 0)
	pjax := r.Header.Get("X-PJAX") == ""
	offset, _ := strconv.Atoi(r.FormValue("offset"))
	query := r.URL.Query().Get("q")
	sidebar := setupProfileSidebar(user, currentUser, "comments")

	post_rows, err := db.Query("SELECT comments.id, post, comments.created_at, comments.edited_at, comments.feeling, comments.body, comments.image, comments.attachment_type, comments.is_spoiler, comments.post_type, comments.url, comments.url_type, comments.pinned, privacy, comments.is_rm_by_admin, nickname, avatar, has_mh, posts.is_rm FROM comments LEFT JOIN posts ON posts.id = post LEFT JOIN users ON posts.created_by = users.id WHERE comments.created_by = ? AND comments.is_rm = 0 AND comments.body LIKE CONCAT('%', ?, '%') AND IF(comments.created_by = ?, true, LOWER(comments.body) NOT REGEXP LOWER(?)) AND (privacy = 0 OR (privacy IN (1, 2, 3, 4) AND (SELECT COUNT(*) FROM friendships WHERE source = ? AND target = posts.created_by OR source = posts.created_by AND target = ? LIMIT 1) = 1) OR (privacy IN (1, 3, 5, 6) AND (SELECT COUNT(*) FROM follows WHERE follow_to = posts.created_by AND follow_by = ? LIMIT 1) = 1) OR (privacy IN (1, 2, 5, 7) AND (SELECT COUNT(*) FROM follows WHERE follow_to = ? AND follow_by = posts.created_by) = 1) OR (privacy = 8 AND ? > 0) OR posts.created_by = ?) ORDER BY comments.id DESC LIMIT 50 OFFSET ?", user.ID, query, currentUser.ID, escapeForbiddenKeywords(currentUser.ForbiddenKeywords), currentUser.ID, currentUser.ID, currentUser.ID, currentUser.ID, currentUser.Level, currentUser.ID, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var posts []*post

	for post_rows.Next() {
		var row = &post{}
		err = post_rows.Scan(&row.ID, &row.CommunityID, &row.CreatedAtTime, &row.EditedAtTime, &row.Feeling, &row.BodyText, &row.Image, &row.AttachmentType, &row.IsSpoiler, &row.PostType, &row.URL, &row.URLType, &row.Pinned, &row.Privacy, &row.IsRMByAdmin, &row.CommunityName, &row.CommunityIcon, &row.PosterHasMii, &row.CommunityRM)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		row.CreatedBy = user.ID
		row.CommunityIcon = getAvatar(row.CommunityIcon, row.PosterHasMii, 0)
		row.CommunityName = "Comment on " + row.CommunityName + "'s Post"
		row.CommentCount = -1
		row.PosterUsername = user.Username
		row.PosterNickname = user.Nickname
		row.PosterIcon = userAvatarBackup
		row.PosterHasMii = user.HasMii
		row.PosterOnline = user.Online
		row.PosterHideOnline = user.HideOnline
		row.PosterColor = user.Color
		row.PosterRoleImage = user.Role.Image
		row = setupPost(row, currentUser, 1, 0)
		posts = append(posts, row)
	}
	post_rows.Close()

	offset += 25

	var data = map[string]interface{}{
		"Title":       user.Nickname + "'s Comments",
		"Pjax":        pjax,
		"Offset":      offset,
		"Query":       query,
		"CurrentUser": currentUser,
		"User":        user,
		"Sidebar":     sidebar,
		"Posts":       posts,
	}
	err = templates.ExecuteTemplate(w, "user_posts.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Show a user's posts.
func showUserPosts(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}

	vars := mux.Vars(r)
	username := vars["username"]
	user := QueryUser(username, currentUser.Timezone)
	if len(user.Username) == 0 || checkIfBlocked(user.ID, currentUser.ID) {
		handle404(w, r)
		return
	}
	userAvatarBackup := user.Avatar
	user.Avatar = getAvatar(user.Avatar, user.HasMii, 0)
	pjax := r.Header.Get("X-PJAX") == ""
	offset, _ := strconv.Atoi(r.FormValue("offset"))
	query := r.URL.Query().Get("q")
	sidebar := setupProfileSidebar(user, currentUser, "posts")

	post_rows, err := db.Query("SELECT posts.id, community_id, created_at, edited_at, feeling, body, image, attachment_type, is_spoiler, post_type, url, url_type, pinned, privacy, repost, migration, migrated_id, migrated_community, is_rm_by_admin, title, icon, rm FROM posts LEFT JOIN communities ON communities.id = community_id WHERE created_by = ? AND is_rm = 0 AND body LIKE CONCAT('%', ?, '%') AND IF(created_by = ?, true, LOWER(body) NOT REGEXP LOWER(?)) AND (privacy = 0 OR (privacy IN (1, 2, 3, 4) AND (SELECT COUNT(*) FROM friendships WHERE source = ? AND target = created_by OR source = created_by AND target = ? LIMIT 1) = 1) OR (privacy IN (1, 3, 5, 6) AND (SELECT COUNT(*) FROM follows WHERE follow_to = created_by AND follow_by = ? LIMIT 1) = 1) OR (privacy IN (1, 2, 5, 7) AND (SELECT COUNT(*) FROM follows WHERE follow_to = ? AND follow_by = created_by) = 1) OR (privacy = 8 AND ? > 0) OR created_by = ?) ORDER BY created_at DESC, posts.id DESC LIMIT 50 OFFSET ?", user.ID, query, currentUser.ID, escapeForbiddenKeywords(currentUser.ForbiddenKeywords), currentUser.ID, currentUser.ID, currentUser.ID, currentUser.ID, currentUser.Level, currentUser.ID, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var posts []*post

	for post_rows.Next() {
		var row = &post{}

		err = post_rows.Scan(&row.ID, &row.CommunityID, &row.CreatedAtTime, &row.EditedAtTime, &row.Feeling, &row.BodyText, &row.Image, &row.AttachmentType, &row.IsSpoiler, &row.PostType, &row.URL, &row.URLType, &row.Pinned, &row.Privacy, &row.RepostID, &row.MigrationID, &row.MigratedID, &row.MigratedCommunity, &row.IsRMByAdmin, &row.CommunityName, &row.CommunityIcon, &row.CommunityRM)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		row.CreatedBy = user.ID
		row.PosterUsername = user.Username
		row.PosterNickname = user.Nickname
		row.PosterIcon = userAvatarBackup
		row.PosterHasMii = user.HasMii
		row.PosterOnline = user.Online
		row.PosterHideOnline = user.HideOnline
		row.PosterColor = user.Color
		row.PosterRoleImage = user.Role.Image
		row = setupPost(row, currentUser, 1, 0)
		posts = append(posts, row)
	}
	post_rows.Close()
	offset += 25

	var data = map[string]interface{}{
		"Title":       user.Nickname + "'s Posts",
		"Pjax":        pjax,
		"Offset":      offset,
		"Query":       query,
		"CurrentUser": currentUser,
		"User":        user,
		"Sidebar":     sidebar,
		"Posts":       posts,
	}
	err = templates.ExecuteTemplate(w, "user_posts.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Search for users.
func showUserSearch(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
	}

	vars := mux.Vars(r)
	query := vars["username"]
	if len(query) == 0 || utf8.RuneCountInString(query) > 32 {
		handle404(w, r)
		return
	}
	pjax := r.Header.Get("X-PJAX") == ""
	offset, _ := strconv.Atoi(r.FormValue("offset"))

	user_rows, err := db.Query("SELECT username, nickname, avatar, has_mh, online, hide_online, color, role, IFNULL(comment, '') FROM users LEFT JOIN profiles ON user = users.id WHERE username LIKE CONCAT('%', ?, '%') OR nickname LIKE CONCAT('%', ?, '%') ORDER BY username ASC LIMIT 20 OFFSET ?", query, query, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var users []*user
	for user_rows.Next() {
		var row = &user{}
		var role int

		err = user_rows.Scan(&row.Username, &row.Nickname, &row.Avatar, &row.HasMii, &row.Online, &row.HideOnline, &row.Color, &role, &row.Comment)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		row.Avatar = getAvatar(row.Avatar, row.HasMii, 0)
		if role > 0 {
			row.Role.Image = getRoleImage(role)
		}

		users = append(users, row)
	}
	user_rows.Close()
	offset += 20

	friendCount, followerCount, followingCount := setupSidebarStatus(currentUser.ID)

	var data = map[string]interface{}{
		"Title":          "Search Users",
		"Pjax":           pjax,
		"CurrentUser":    currentUser,
		"FriendCount":    friendCount,
		"FollowingCount": followingCount,
		"FollowerCount":  followerCount,
		"Query":          query,
		"Action":         "/users",
		"Offset":         offset,
		"Users":          users,
	}
	err = templates.ExecuteTemplate(w, "search.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// Show user yeahs
func showUserYeahs(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}

	vars := mux.Vars(r)
	username := vars["username"]
	user := QueryUser(username, currentUser.Timezone)
	if len(user.Username) == 0 || checkIfBlocked(user.ID, currentUser.ID) {
		handle404(w, r)
		return
	}
	user.Avatar = getAvatar(user.Avatar, user.HasMii, 0)

	pjax := r.Header.Get("X-PJAX") == ""
	offset, _ := strconv.Atoi(r.FormValue("offset"))
	query := r.URL.Query().Get("q")
	sidebar := setupProfileSidebar(user, currentUser, "yeahs")

	post_rows, err := db.Query("SELECT posts.id, created_by, community_id, created_at, edited_at, feeling, body, image, attachment_type, is_spoiler, post_type, url, url_type, pinned, privacy, repost, migration, migrated_id, migrated_community, is_rm, is_rm_by_admin, username, nickname, avatar, has_mh, online, hide_online, color, role, title, icon, rm, nigga, type FROM (SELECT posts.id, posts.created_by, posts.community_id, posts.created_at, posts.edited_at, posts.feeling, posts.body, posts.image, posts.attachment_type, posts.is_spoiler, posts.post_type, posts.url, posts.url_type, posts.pinned, posts.privacy, repost, migration, migrated_id, migrated_community, is_rm, is_rm_by_admin, users.username, users.nickname, users.avatar, users.has_mh, users.online, users.hide_online, users.color, users.role, title, icon, rm, 0 nigga, 0 type FROM posts LEFT JOIN users ON posts.created_by = users.id LEFT JOIN communities ON community_id = communities.id UNION SELECT comments.id, comments.created_by, post, comments.created_at, comments.edited_at, comments.feeling, comments.body, comments.image, comments.attachment_type, comments.is_spoiler, comments.post_type, comments.url, comments.url_type, comments.pinned, op.privacy, 0, 0, 0, 0, comments.is_rm, comments.is_rm_by_admin, creator.username, creator.nickname, creator.avatar, creator.has_mh, creator.online, creator.hide_online, creator.color, creator.role, poster.nickname, poster.avatar, op.is_rm, poster.has_mh, 1 FROM comments LEFT JOIN posts AS op ON post = op.id LEFT JOIN users AS creator ON comments.created_by = creator.id LEFT JOIN users AS poster ON op.created_by = poster.id) posts LEFT JOIN yeahs ON yeah_post = posts.id WHERE yeah_by = ? AND on_comment = type AND body LIKE CONCAT('%', ?, '%') AND 	is_rm = 0 AND is_rm_by_admin = 0 AND created_by NOT IN (SELECT if(source = ?, target, source) FROM blocks WHERE (source = ? AND target = created_by) OR (source = created_by AND target = ?)) AND IF(created_by = ?, true, LOWER(body) NOT REGEXP LOWER(?)) AND (privacy = 0 OR (privacy IN (1, 2, 3, 4) AND (SELECT COUNT(*) FROM friendships WHERE source = ? AND target = created_by OR source = created_by AND target = ? LIMIT 1) = 1) OR (privacy IN (1, 3, 5, 6) AND (SELECT COUNT(*) FROM follows WHERE follow_to = created_by AND follow_by = ? LIMIT 1) = 1) OR (privacy IN (1, 2, 5, 7) AND (SELECT COUNT(*) FROM follows WHERE follow_to = ? AND follow_by = created_by) = 1) OR (privacy = 8 AND ? > 0) OR created_by = ?) ORDER BY yeahs.id DESC LIMIT 25 OFFSET ?", user.ID, query, currentUser.ID, currentUser.ID, currentUser.ID, currentUser.ID, escapeForbiddenKeywords(currentUser.ForbiddenKeywords), currentUser.ID, currentUser.ID, currentUser.ID, currentUser.ID, currentUser.Level, currentUser.ID, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var posts []*post

	for post_rows.Next() {
		var row = &post{}
		var communityHasMii bool
		var onComment bool

		err = post_rows.Scan(&row.ID, &row.CreatedBy, &row.CommunityID, &row.CreatedAtTime, &row.EditedAtTime, &row.Feeling, &row.BodyText, &row.Image, &row.AttachmentType, &row.IsSpoiler, &row.PostType, &row.URL, &row.URLType, &row.Pinned, &row.Privacy, &row.RepostID, &row.MigrationID, &row.MigratedID, &row.MigratedCommunity, &row.IsRM, &row.IsRMByAdmin, &row.PosterUsername, &row.PosterNickname, &row.PosterIcon, &row.PosterHasMii, &row.PosterOnline, &row.PosterHideOnline, &row.PosterColor, &row.PosterRoleID, &row.CommunityName, &row.CommunityIcon, &row.CommunityRM, &communityHasMii, &onComment)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if onComment {
			row.CommunityIcon = getAvatar(row.CommunityIcon, communityHasMii, 0)
			row.CommunityName = "Comment on " + row.CommunityName + "'s Post"
			row.CommentCount = -1
		}
		row = setupPost(row, currentUser, 1, 0)
		posts = append(posts, row)
	}
	post_rows.Close()

	offset += 25

	var data = map[string]interface{}{
		"Title":       user.Nickname + "'s Yeahs",
		"Pjax":        pjax,
		"Offset":      offset,
		"Query":       query,
		"CurrentUser": currentUser,
		"User":        user,
		"Sidebar":     sidebar,
		"Posts":       posts,
	}
	err = templates.ExecuteTemplate(w, "user_posts.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Unblock a user.
func unblockUser(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	vars := mux.Vars(r)

	username := vars["username"]

	if username != currentUser.Username {
		var user_id int
		var usern string
		db.QueryRow("SELECT id, username FROM users WHERE username = ?", username).Scan(&user_id, &usern)
		if len(usern) == 0 {
			handle404(w, r)
			return
		}

		stmt, err := db.Prepare("DELETE FROM blocks WHERE source = ? AND target = ?")
		if err == nil {
			// If there's no errors, we can go ahead and execute the statement.
			_, err := stmt.Exec(&currentUser.ID, &user_id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		var msg wsMessage
		msg.Type = "unblock"
		msg.Content = currentUser.Username
		for client := range clients {
			//clients[client].Mutex.Lock()
			if clients[client].UserID == user_id {
				err := writeWs(clients[client], client, msg)
				if err != nil {
					client.Close()
					delete(clients, client)
				}
			}
			//clients[client].Mutex.Unlock()
		}
	}
}

// Unfavorite a post.
func unfavoritePost(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	vars := mux.Vars(r)
	post_id := vars["id"]

	var sex int
	err := db.QueryRow("SELECT COUNT(*) FROM profiles WHERE favorite = ? AND user = ?", post_id, currentUser.ID).Scan(&sex)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if sex == 0 {
		handle404(w, r)
		return
	}

	stmt, err := db.Prepare("UPDATE profiles SET favorite = 0 WHERE user = ?")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, err = stmt.Exec(&currentUser.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// Upload an image.
func uploadImage(w http.ResponseWriter, r *http.Request) {
	if len(r.FormValue("file")) == 0 {
		http.Error(w, "You must upload a file.", http.StatusBadRequest)
		return
	}
	h := md5.New()
	io.WriteString(h, r.FormValue("file"))
	var imageID sql.NullString
	hash := hex.EncodeToString(h.Sum(nil))
	db.QueryRow("SELECT id FROM images WHERE hash = ?", hash).Scan(&imageID)
	if imageID.Valid {
		w.Write([]byte(imageID.String))
		return
	}

	switch settings.ImageHost.Provider {
	case "cloudinary":
		bodyData := r.FormValue("file")

		data := url.Values{}
		data.Set("upload_preset", settings.ImageHost.UploadPreset)
		data.Add("file", bodyData)

		resp, err := http.Post(settings.ImageHost.APIEndpoint+"/v1_1/"+settings.ImageHost.Username+"/auto/upload", "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonBody := make(map[string]interface{})
		json.Unmarshal(body, &jsonBody)

		if image, ok := jsonBody["secure_url"].(string); ok {
			_, err = db.Exec("INSERT INTO images (value, hash) VALUES (?, ?)", image, hash)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			var image_id string
			err = db.QueryRow("SELECT id FROM images WHERE value = ? ORDER BY id DESC LIMIT 1", image).Scan(&image_id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Write([]byte(image_id))
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		/*	case "lambda": // WIP
				file := &bytes.Buffer{}
				writer := multipart.NewWriter(file)
				part, err := writer.CreateFormFile("file", "indigo.jpg")
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}

				_, err = io.Copy(part, buffer)
				writer.Close()
				req, err := http.NewRequest("POST", settings.ImageHost.APIEndpoint + "/api/upload", buffer)
				req.Header.Set("Content-Type", writer.FormDataContentType())
				client := &http.Client{}
				resp, err := client.Do(req)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				_, err = buffer.ReadFrom(resp.Body)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				resp.Body.Close()

				if(buffer.String() == "500") {
					http.Error(w, buffer.String(), http.StatusInternalServerError)
					return
				}
				jsonBody := make(map[string]interface{})
				err = json.Unmarshal(buffer.Bytes(), &jsonBody)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				files := jsonBody["files"].([]interface{})
				thingy := files[0].(map[string]interface{})
				w.Write([]byte(thingy["url"].(string)))
				//w.Write(body.Bytes())
			case "pomf": // WIP
				file := &bytes.Buffer{}
				writer := multipart.NewWriter(file)
				part, err := writer.CreateFormFile("files[]", "indigo.png")
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}

				_, err = io.Copy(part, buffer)
				writer.Close()
				req, err := http.NewRequest("POST", settings.ImageHost.APIEndpoint + "/upload.php", buffer)
				req.Header.Set("Content-Type", writer.FormDataContentType())
				client := &http.Client{}
				resp, err := client.Do(req)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}
				_, err = buffer.ReadFrom(resp.Body)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}
				resp.Body.Close()

				jsonBody := make(map[string]interface{})
				json.Unmarshal(buffer.Bytes(), &jsonBody)
				files := jsonBody["files"].([]interface{})
				thingy := files[0].(map[string]interface{})
				w.Write([]byte(thingy["url"].(string)))
				//w.Write(body.Bytes())*/
	}
}

// Vote on a poll.
func voteOnPoll(w http.ResponseWriter, r *http.Request) {
	currentUser, success := doSession(w, r)
	if !success {
		return
	}
	if len(currentUser.Username) == 0 {
		http.Redirect(w, r, "/login", 301)
		return
	}

	vars := mux.Vars(r)
	postID := vars["id"]
	optionID := r.FormValue("option")

	var msg wsMessage
	var count int
	if optionID != "0" {
		err = db.QueryRow("SELECT COUNT(*) FROM options WHERE post = ? AND id = ?", postID, optionID).Scan(&count)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if count == 0 {
			http.Error(w, "That option does not exist.", http.StatusBadRequest)
			return
		}
		option_id := -1
		db.QueryRow("SELECT option_id FROM votes WHERE poll = ? AND user = ?", postID, currentUser.ID).Scan(&option_id)
		if option_id != -1 {
			db.Exec("UPDATE votes SET option_id = ? WHERE poll = ? AND user = ?", optionID, postID, currentUser.ID)
			msg.Type = "pollChange"
			msg.Content = strconv.Itoa(option_id)
		} else {
			_, err = db.Exec("INSERT INTO votes (option_id, user, poll) VALUES (?, ?, ?)", optionID, currentUser.ID, postID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			msg.Type = "pollVote"
		}
		msg.ID = optionID
	} else {
		db.QueryRow("SELECT COUNT(*) FROM votes WHERE poll = ? AND user = ?", postID, currentUser.ID).Scan(&count)
		if count == 0 {
			return
		} else {
			db.QueryRow("SELECT option_id FROM votes WHERE poll = ? AND user = ?", postID, currentUser.ID).Scan(&msg.ID)
			db.Exec("DELETE FROM votes WHERE poll = ? AND user = ?", postID, currentUser.ID)
			msg.Type = "pollUnvote"
		}
	}

	for client := range clients {
		//clients[client].Mutex.Lock()
		if clients[client].UserID != currentUser.ID {
			err := writeWs(clients[client], client, msg)
			if err != nil {
				client.Close()
				delete(clients, client)
			}
		}
		//clients[client].Mutex.Unlock()
	}
}
