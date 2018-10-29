// Various utility functions used in Indigo.

package main

import (
	// Internals
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"math"
	"math/rand"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	// Externals
	"github.com/gorilla/csrf"
	"github.com/gorilla/websocket"
	sessions "github.com/kataras/go-sessions"
	"github.com/microcosm-cc/bluemonday"
	"gopkg.in/russross/blackfriday.v2"
)

// Inititialize sessions and other variables. Used in almost every page that uses HTML, and even some that don't.
func doSession(w http.ResponseWriter, r *http.Request) (user, bool) {
	session := sessions.Start(w, r)
	currentUser := user{}
	ip := getIP(r)

	timezone, err := r.Cookie("timezone")
	if err != nil || len(timezone.Value) == 0 {
		timezone = &http.Cookie{Name: "timezone", Value: getTimezone(ip), Expires: time.Now().Add(365 * 24 * time.Hour)}
		http.SetCookie(w, timezone)
	}
	currentUser.Timezone = timezone.Value

	host, _, _ := net.SplitHostPort(ip)
	cidr := getCIDR(host)
	var banLength time.Time
	db.QueryRow("SELECT until FROM bans WHERE (cidr = 0 AND ip = ?) OR (cidr = 1 AND ip = ?)", host, cidr).Scan(&banLength)
	if int64(banLength.Unix()) != -62135596800 {
		success := showBan(w, currentUser, banLength)
		if success {
			return currentUser, false
		}
	}
	if len(session.GetString("username")) != 0 {
		currentUser = QueryUser(session.GetString("username"), currentUser.Timezone)
		if len(currentUser.Theme) > 0 {
			currentUser.ThemeColors = strings.Split(currentUser.Theme, ",")
		}
		currentUser.Avatar = getAvatar(currentUser.Avatar, currentUser.HasMii, 0)

		db.QueryRow("SELECT until FROM bans WHERE user = ?", currentUser.ID).Scan(&banLength)
		if int64(banLength.Unix()) != -62135596800 {
			success := showBan(w, currentUser, banLength)
			if success {
				return currentUser, false
			}
		}
	} else {
		indigoAuth, err := r.Cookie("indigo-auth")
		if err == nil && len(indigoAuth.Value) > 0 {
			var sexy string
			db.QueryRow("SELECT username FROM login_tokens LEFT JOIN users ON user = users.id WHERE value = ?", &indigoAuth.Value).Scan(&sexy)
			if len(sexy) > 0 {
				currentUser = QueryUser(sexy, currentUser.Timezone)
				if len(currentUser.Theme) > 0 {
					currentUser.ThemeColors = strings.Split(currentUser.Theme, ",")
				}
				currentUser.Avatar = getAvatar(currentUser.Avatar, currentUser.HasMii, 0)

				session.Set("username", currentUser.Username)
				session.Set("user_id", currentUser.ID)
				stmt, err := db.Prepare("INSERT INTO sessions (id, user) VALUES (?, ?)")
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return currentUser, false
				}
				stmt.Exec(session.ID(), currentUser.ID)

				db.QueryRow("SELECT until FROM bans WHERE user = ?", currentUser.ID).Scan(&banLength)
				if int64(banLength.Unix()) != -62135596800 {
					success := showBan(w, currentUser, banLength)
					if success {
						return currentUser, false
					}
				}
			} else {
				if settings.ForceLogins && r.URL.Path != "/reset" {
					http.Redirect(w, r, "/login", 301)
					return currentUser, false
				}
				return currentUser, true
			}
		} else {
			if settings.ForceLogins && r.URL.Path != "/reset" {
				http.Redirect(w, r, "/login", 301)
				return currentUser, false
			}
			return currentUser, true
		}
	}

	currentUser.CSRFToken = csrf.Token(r)
	currentUser.LightMode = getLightMode(w, r)

	if r.Header.Get("X-PJAX") == "" {
		var friendRequests int
		db.QueryRow("SELECT COUNT(*) FROM messages LEFT JOIN conversations ON conversation_id = conversations.id WHERE (source = ? OR target = ?) AND created_by <> ? AND msg_read = 0 AND messages.is_rm = 0 AND conversations.is_rm = 0", currentUser.ID, currentUser.ID, currentUser.ID).Scan(&currentUser.Notifications.Messages)
		var groupUnread int
		db.QueryRow("SELECT SUM(unread_messages) FROM group_members WHERE user = ?", currentUser.ID).Scan(&groupUnread)
		currentUser.Notifications.Messages += groupUnread
		db.QueryRow("SELECT COUNT(*) FROM notifications WHERE notif_to = ? AND merged IS NULL AND notif_read = 0", currentUser.ID).Scan(&currentUser.Notifications.Notifications)
		db.QueryRow("SELECT COUNT(*) FROM friend_requests WHERE request_to = ? AND request_read = 0", currentUser.ID).Scan(&friendRequests)
		currentUser.Notifications.Notifications += friendRequests
	}

	return currentUser, true
}

// Escape the "forbidden keywords" field for regexp.
func escapeForbiddenKeywords(regex string) string {
	if len(regex) == 0 {
		return "b\bb" // This regex always returns "false", so no posts are ever filtered out if you don't have any reserved words.
	}
	regex = regexp.QuoteMeta(regex)
	split := strings.Split(regex, ",")
	fixed := []string{}
	for _, s := range split {
		if len(s) > 0 {
			fixed = append(fixed, s)
		}
	}
	regex = strings.Join(fixed, "|")
	regex = strings.Replace(regex, "\\|", ",", -1)
	return strings.Replace(regex, "\\\\", "\\", -1)
}

// Escape Markdown.
func escapeMarkdown(text string) string {
	text = string(symbols.ReplaceAll([]byte(text), []byte("\\$1")))
	return text
}

func writeWs(session wsSession, client *websocket.Conn, message wsMessage) error {
	//session.Mutex.Lock()
	//defer session.Mutex.Unlock()
	return client.WriteJSON(message)
}

// Get a CIDR-esque range from an IP.
func getCIDR(ip string) string {
	netmasks := strings.Split(ip, ".")
	netmasks[3] = "0"
	return strings.Join(netmasks, ".")
}

// Get the user's light mode status.
func getLightMode(w http.ResponseWriter, r *http.Request) bool {
	lightMode, err := r.Cookie("light")
	if err != nil || len(lightMode.Value) == 0 {
		lightMode = &http.Cookie{Name: "light", Value: "false", Expires: time.Unix(253402300799, 0)}
		http.SetCookie(w, lightMode)
	}
	lightModeBool, err := strconv.ParseBool(lightMode.Value)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return false
	}
	return lightModeBool
}

// Get the data of a post's migration.
func getPostMigration(migration int, migratedCommunity string) (string, string, string, string) {
	var migrationImage string
	var migrationURL string
	err := db.QueryRow("SELECT image, url FROM migrations WHERE id = ?", migration).Scan(&migrationImage, &migrationURL)
	if err != nil {
		fmt.Println("no migrations")
		fmt.Println(err.Error())
		return "https://i.ytimg.com/vi/DkIVqD8pJt8/maxresdefault.jpg", "http://marios-princess-sex.ga/#", "This message should not appear. An error occurred while grabbing the migration data. Check the console.", "https://closed.pizza/s/img/title-icon-default.png"
	}

	var communityTitle string
	var communityIcon string
	err = db.QueryRow("SELECT title, icon FROM migrated_communities WHERE migrated_id = ? AND migration = ?", migratedCommunity, migration).Scan(&communityTitle, &communityIcon)
	if err != nil {
		return migrationImage, migrationURL, "Unknown Community", "/assets/img/title-icon-default.png"
	}

	return migrationImage, migrationURL, communityTitle, communityIcon
}

// Format timestamps in a way that normal people who AREN'T robots can read.
func humanTiming(timestamp time.Time, timezone string) string {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		fmt.Println(err.Error())
		return err.Error()
	}
	timestamp = timestamp.In(location)
	since := time.Now().In(location).Sub(timestamp).Seconds()
	if since <= 1 {
		return "Less than a second ago"
	} else if since < 2 {
		return "1 second ago"
	} else if since < 60 {
		return strconv.Itoa(int(math.Floor(since))) + " seconds ago"
	} else if since < 120 {
		return "1 minute ago"
	} else if since < 3600 {
		return strconv.Itoa(int(math.Floor(since/60))) + " minutes ago"
	} else if since < 7200 {
		return "1 hour ago"
	} else if since < 86400 {
		return strconv.Itoa(int(math.Floor(since/60/60))) + " hours ago"
	} else if since < 172800 {
		return "1 day ago"
	} else if since < 345600 {
		return strconv.Itoa(int(math.Floor(since/60/60/24))) + " days ago"
	} else {
		return timestamp.Format("01/02/2006 3:04 PM")
	}
	/* Discord styled timestamp code here.
	now := time.Now().In(location)
	if now.Day() == timestamp.Day() && now.Month() == timestamp.Month() && now.Year() == timestamp.Year() {
		return timestamp.Format("Today at 3:04 PM")
	} else if now.Day()-1 == timestamp.Day() && now.Month() == timestamp.Month() && now.Year() == timestamp.Year() {
		return timestamp.Format("Yesterday at 3:04 PM")
	} else if now.Day()-2 == timestamp.Day() && now.Month() == timestamp.Month() && now.Year() == timestamp.Year() {
		return timestamp.Format("Last Monday at 3:04 PM")
	} else {
		return timestamp.Format("01/02/2006 3:04 PM")
	}
	*/
}

// Send a notification to a user.
func createNotif(to int, notif_type int, post string, currentUser int) {
	notif_read := 0
	for client := range clients {
		clients[client].Mutex.Lock()
		if clients[client].UserID == to {
			if ((notif_type == 0 || notif_type == 2 || notif_type == 3) && clients[client].OnPage == "/posts/"+post) || (notif_type == 1 && clients[client].OnPage == "/comments/"+post) {
				notif_read = 1
				break
			}
		}
		//clients[client].Mutex.Unlock()
	}

	if notif_type == 0 || notif_type == 1 {
		var nya bool
		db.QueryRow("SELECT yeah_notifications FROM users WHERE id = ?", to).Scan(&nya)
		if !nya {
			return
		}
	}

	// 0 = post yeah, 1 = reply yeah, 2 = comment on your post, 3 = poster's comment, 4 = follow
	var check_mergedusernews int
	if notif_type != 4 {
		db.QueryRow("SELECT merged FROM notifications WHERE notif_by = ? AND notif_to = ? AND notif_type = ? AND notif_post = ? AND merged IS NOT NULL AND notif_date > NOW() - 28800 ORDER BY notif_date DESC", currentUser, to, notif_type, post).Scan(&check_mergedusernews)
	} else {
		db.QueryRow("SELECT merged FROM notifications WHERE notif_by = ? AND notif_to = ? AND notif_type = ? AND merged IS NOT NULL AND notif_date > NOW() - 28800 ORDER BY notif_date DESC", currentUser, to, notif_type).Scan(&check_mergedusernews)
	}
	if check_mergedusernews != 0 {
		stmt, _ := db.Prepare("UPDATE notifications SET notif_read = 0, notif_date = CURRENT_TIMESTAMP WHERE id = ?")
		stmt.Exec(&check_mergedusernews)
	} else {
		var result_update_newsmergesearch int
		if notif_type != 4 {
			db.QueryRow("SELECT id FROM notifications WHERE notif_to = ? AND notif_post = ? AND notif_date > NOW() - 28800 AND notif_type = ? ORDER BY notif_date DESC", to, post, notif_type).Scan(&result_update_newsmergesearch)
		} else {
			db.QueryRow("SELECT id FROM notifications WHERE notif_to = ? AND notif_date > NOW() - 28800 AND notif_type = ? ORDER BY notif_date DESC", to, notif_type).Scan(&result_update_newsmergesearch)
		}
		if result_update_newsmergesearch != 0 {
			if notif_type != 4 {
				stmt, _ := db.Prepare("INSERT INTO notifications(notif_by, notif_to, notif_post, merged, notif_type, notif_read) VALUES (?, ?, ?, ?, ?, ?)")
				stmt.Exec(currentUser, to, post, result_update_newsmergesearch, notif_type, notif_read)
			} else {
				stmt, _ := db.Prepare("INSERT INTO notifications(notif_by, notif_to, merged, notif_type, notif_read) VALUES (?, ?, ?, ?, ?)")
				stmt.Exec(currentUser, to, result_update_newsmergesearch, notif_type, notif_read)
			}
			stmt, _ := db.Prepare("UPDATE notifications SET notif_read = ?, notif_date = NOW() WHERE id = ?")
			stmt.Exec(notif_read, result_update_newsmergesearch)
		} else {
			if notif_type != 4 {
				stmt, _ := db.Prepare("INSERT INTO notifications(notif_by, notif_to, notif_post, notif_type, notif_read) VALUES (?, ?, ?, ?, ?)")
				stmt.Exec(currentUser, to, post, notif_type, notif_read)
			} else {
				stmt, _ := db.Prepare("INSERT INTO notifications(notif_by, notif_to, notif_type, notif_read) VALUES (?, ?, ?, ?)")
				stmt.Exec(currentUser, to, notif_type, notif_read)
			}
		}
	}

	if notif_read == 0 {
		var msg wsMessage
		msg.Type = "notif"
		var notifCount int
		var friendRequests int
		db.QueryRow("SELECT COUNT(*) FROM notifications WHERE notif_to = ? AND merged IS NULL AND notif_read = 0", &to).Scan(&notifCount)
		db.QueryRow("SELECT COUNT(*) FROM friend_requests WHERE request_to = ? AND request_read = 0", to).Scan(&friendRequests)
		msg.Content = strconv.Itoa(notifCount + friendRequests)
		for client := range clients {
			clients[client].Mutex.Lock()
			if clients[client].UserID == to {
				err := client.WriteJSON(msg)
				if err != nil {
					client.Close()
					delete(clients, client)
				}
			}
			//clients[client].Mutex.Unlock()
		}
	}
}

// Find a user with a username.
func QueryUser(username string, timezone string) user {
	var users = user{}
	var role int
	var lastSeenTime time.Time
	db.QueryRow("SELECT id, username, nickname, avatar, has_mh, email, password, ip, level, role, online, hide_online, last_seen, hide_last_seen, color, theme, yeah_notifications, websockets_enabled, forbidden_keywords, default_privacy FROM users WHERE username=?", username).Scan(&users.ID, &users.Username, &users.Nickname, &users.Avatar, &users.HasMii, &users.Email, &users.Password, &users.IP, &users.Level, &role, &users.Online, &users.HideOnline, &lastSeenTime, &users.HideLastSeen, &users.Color, &users.Theme, &users.YeahNotifications, &users.WebsocketsEnabled, &users.ForbiddenKeywords, &users.DefaultPrivacy)

	if role > 0 {
		db.QueryRow("SELECT image, organization FROM roles WHERE id = ?", role).Scan(&users.Role.Image, &users.Role.Organization)
	}
	users.Timezone = timezone
	users.LastSeen = humanTiming(lastSeenTime, timezone)
	users.LastSeenUnix = lastSeenTime.Unix()

	return users
}

// Get an array of posts from an SQL query.
func setupPost(row *post, currentUser user, postType int, repostLayer int) *post {
	row.PosterIcon = getAvatar(row.PosterIcon, row.PosterHasMii, row.Feeling)
	if row.PosterRoleID > 0 {
		row.PosterRoleImage = getRoleImage(row.PosterRoleID)
	}

	row.CreatedAt = humanTiming(row.CreatedAtTime, currentUser.Timezone)
	row.CreatedAtUnix = row.CreatedAtTime.Unix()
	if row.EditedAtTime.Sub(row.CreatedAtTime).Minutes() > 5 {
		row.EditedAt = humanTiming(row.EditedAtTime, currentUser.Timezone)
		row.EditedAtUnix = row.EditedAtTime.Unix()
	}
	if len(row.MigratedID) == 0 || strings.Contains(row.BodyText, ":markdown:") {
		row.Body = parseBodyWithLineBreaks(row.BodyText, true, true)
	} else {
		row.Body = parseBodyWithLineBreaks(row.BodyText, true, false)
	}
	if row.CreatedBy == currentUser.ID {
		row.ByMe = true
	}
	if row.PostType == 2 {
		row.Poll = getPoll(row.ID, currentUser.ID)
	}
	row.Type = postType
	if row.RepostID > 0 {
		var repost post
		if repostLayer < 3 {
			db.QueryRow("SELECT posts.id, created_by, created_at, edited_at, feeling, body, image, attachment_type, is_spoiler, post_type, url, url_type, pinned, privacy, repost, migration, migrated_id, migrated_community, is_rm_by_admin, communities.id, title, icon, rm, username, nickname, avatar, has_mh, online, hide_online, color, role FROM posts LEFT JOIN communities ON communities.id = community_id LEFT JOIN users ON users.id = created_by WHERE posts.id = ? AND is_rm = 0 AND users.id NOT IN (SELECT if(source = ?, target, source) FROM blocks WHERE (source = ? AND target = users.id) OR (source = users.id AND target = ?)) AND IF(created_by = ?, true, LOWER(body) NOT REGEXP LOWER(?)) AND (privacy = 0 OR (privacy IN (1, 2, 3, 4) AND (SELECT COUNT(*) FROM friendships WHERE source = ? AND target = created_by OR source = created_by AND target = ? LIMIT 1) = 1) OR (privacy IN (1, 3, 5, 6) AND (SELECT COUNT(*) FROM follows WHERE follow_to = created_by AND follow_by = ? LIMIT 1) = 1) OR (privacy IN (1, 2, 5, 7) AND (SELECT COUNT(*) FROM follows WHERE follow_to = ? AND follow_by = created_by) = 1) OR (privacy = 8 AND ? > 0) OR created_by = ?) LIMIT 1", row.RepostID, currentUser.ID, currentUser.ID, currentUser.ID, currentUser.ID, escapeForbiddenKeywords(currentUser.ForbiddenKeywords), currentUser.ID, currentUser.ID, currentUser.ID, currentUser.ID, currentUser.Level, currentUser.ID).Scan(&repost.ID, &repost.CreatedBy, &repost.CreatedAtTime, &repost.EditedAtTime, &repost.Feeling, &repost.BodyText, &repost.Image, &repost.AttachmentType, &repost.IsSpoiler, &repost.PostType, &repost.URL, &repost.URLType, &repost.Pinned, &repost.Privacy, &repost.RepostID, &repost.MigrationID, &repost.MigratedID, &repost.MigratedCommunity, &repost.IsRMByAdmin, &repost.CommunityID, &repost.CommunityName, &repost.CommunityIcon, &repost.CommunityRM, &repost.PosterUsername, &repost.PosterNickname, &repost.PosterIcon, &repost.PosterHasMii, &repost.PosterOnline, &repost.PosterHideOnline, &repost.PosterColor, &repost.PosterRoleID)
			row.Repost = &repost
			row.Repost.Type = 3
			if len(row.Repost.CommunityName) > 0 {
				repostLayer = repostLayer + 1
				row.Repost = setupPost(row.Repost, currentUser, 3, repostLayer)
			}
		} else {
			repost.Type = 4
			repost.ID = row.ID
			row.Repost = &repost
		}
	}

	if row.MigrationID > 0 {
		row.MigrationImage, row.MigrationURL, row.CommunityName, row.CommunityIcon = getPostMigration(row.MigrationID, row.MigratedCommunity)
	}

	db.QueryRow("SELECT COUNT(*) FROM yeahs WHERE yeah_post = ? AND yeah_by = ? AND on_comment = 0 LIMIT 1", row.ID, currentUser.ID).Scan(&row.Yeahed)
	row.CanYeah = checkIfCanYeah(currentUser, row.CreatedBy)

	if row.CommentCount != -1 {
		db.QueryRow("SELECT COUNT(*) FROM yeahs WHERE yeah_post = ? AND on_comment = 0", row.ID).Scan(&row.YeahCount)
		db.QueryRow("SELECT COUNT(*) FROM comments WHERE post = ? AND is_rm = 0", row.ID).Scan(&row.CommentCount)
		if row.CommentCount > 0 && postType != -1 && postType != 3 {
			row.CommentPreview = getCommentPreview(row.ID, currentUser)
		}
	} else {
		db.QueryRow("SELECT COUNT(*) FROM yeahs WHERE yeah_post = ? AND on_comment = 1", row.ID).Scan(&row.YeahCount)
	}

	return row
}

// Show a ban screen.
func showBan(w http.ResponseWriter, currentUser user, banLength time.Time) bool {
	if time.Now().Sub(banLength).Seconds() > 1 {
		stmt, _ := db.Prepare("DELETE FROM bans WHERE user = ?")
		stmt.Exec(currentUser.ID)
		return false
	} else {
		var data = map[string]interface{}{
			"CurrentUser": currentUser,
			"Length":      banLength.Format("01/02/2006 3:04 PM"),
		}
		err := templates.ExecuteTemplate(w, "ban.html", data)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return true
	}
}

// Find a profile by user ID.
func QueryProfile(id int, timezone string) profile {
	var profiles = profile{}
	var createdAtTime time.Time
	var genderNumber int // Gender is just a number.
	db.QueryRow("SELECT created_at, nnid, mh, avatar_image, avatar_id, gender, region, comment, nnid_visibility, yeah_visibility, reply_visibility, discord, steam, psn, switch_code, twitter, youtube, allow_friend, favorite FROM profiles WHERE user = ?", id).Scan(&createdAtTime, &profiles.NNID, &profiles.MiiHash, &profiles.AvatarImage, &profiles.AvatarID, &genderNumber, &profiles.Region, &profiles.CommentText, &profiles.NNIDVisibility, &profiles.YeahVisibility, &profiles.ReplyVisibility, &profiles.Discord, &profiles.Steam, &profiles.PSN, &profiles.SwitchCode, &profiles.Twitter, &profiles.YouTube, &profiles.AllowFriend, &profiles.FavoritePostID)

	profiles.Gender = [6]string{"", "He/him", "She/her", "He/she", "Nonbinary", "They/them"}[genderNumber]
	profiles.User = id
	profiles.Comment = parseBodyWithLineBreaks(profiles.CommentText, false, true)
	profiles.CreatedAt = humanTiming(createdAtTime, timezone)
	profiles.CreatedAtUnix = createdAtTime.Unix()
	return profiles
}

// Find a community by ID.
func QueryCommunity(id string, canBeRM bool) community {
	var communities = community{}
	if canBeRM {
		db.QueryRow("SELECT id, title, description, icon, banner, is_featured, permissions, rm FROM communities WHERE id = ?", id).Scan(&communities.ID, &communities.Title, &communities.DescriptionText, &communities.Icon, &communities.Banner, &communities.IsFeatured, &communities.Permissions, &communities.RM)
	} else {
		db.QueryRow("SELECT id, title, description, icon, banner, is_featured, permissions, rm FROM communities WHERE id = ? AND rm = 0", id).Scan(&communities.ID, &communities.Title, &communities.DescriptionText, &communities.Icon, &communities.Banner, &communities.IsFeatured, &communities.Permissions, &communities.RM)
	}

	communities.Description = parseBodyWithLineBreaks(communities.DescriptionText, false, true)
	return communities
}

// Set variables for profile sidebar.
func setupProfileSidebar(user user, currentUser user, profileOnPage string) profileSidebar {
	var sidebar profileSidebar
	sidebar.Profile = QueryProfile(user.ID, currentUser.Timezone)
	sidebar.User = user
	sidebar.CurrentUser = currentUser
	sidebar.ProfileOnPage = profileOnPage
	sidebar.Reasons = settings.ReportReasons

	if len(sidebar.User.Theme) > 0 {
		sidebar.User.ThemeColors = strings.Split(sidebar.User.Theme, ",")
	}

	db.QueryRow("SELECT COUNT(*) FROM follows WHERE follow_to = ? AND follow_by = ? LIMIT 1", user.ID, currentUser.ID).Scan(&sidebar.IsFollowing)
	var requestTimestamp time.Time
	_ = db.QueryRow("SELECT COUNT(*), created_at FROM friend_requests WHERE request_to = ? AND request_by = ? GROUP BY created_at", user.ID, currentUser.ID).Scan(&sidebar.FriendStatus, &requestTimestamp)
	if sidebar.FriendStatus > 0 {
		sidebar.FriendStatus = 2
		sidebar.RequestTime = requestTimestamp.Format("01/02/2006 3:04 PM")
	} else {
		db.QueryRow("SELECT COUNT(*) FROM friend_requests WHERE request_to = ? AND request_by = ?", currentUser.ID, user.ID).Scan(&sidebar.FriendStatus)
		if sidebar.FriendStatus > 0 {
			sidebar.FriendStatus = 1
			var createdAt time.Time
			db.QueryRow("SELECT id, message, created_at FROM friend_requests WHERE request_to = ? AND request_by = ? ORDER BY friend_requests.id DESC", currentUser.ID, user.ID).Scan(&sidebar.Request.ID, &sidebar.Request.Message, &createdAt)
			sidebar.Request.CreatedAt = createdAt.Format("01/02/2006 3:04 PM")
		} else {
			db.QueryRow("SELECT COUNT(*) FROM friendships WHERE (source = ? AND target = ?) OR (source = ? AND target = ?)", user.ID, currentUser.ID, currentUser.ID, user.ID).Scan(&sidebar.FriendStatus)
			if sidebar.FriendStatus > 0 {
				sidebar.FriendStatus = 3
			} else {
				sidebar.FriendStatus = 0
				if sidebar.Profile.AllowFriend == 1 {
					db.QueryRow("SELECT COUNT(*) FROM follows WHERE follow_to = ? AND follow_by = ? LIMIT 1", currentUser.ID, user.ID).Scan(&sidebar.IsFollowingMe)
				}
			}
		}
	}
	sidebar.User.Blocked = checkIfBlocked(currentUser.ID, user.ID)
	sidebar.Profile.FriendCount, sidebar.Profile.FollowingCount, sidebar.Profile.FollowerCount = setupSidebarStatus(user.ID)
	var banCount int
	db.QueryRow("SELECT COUNT(*) FROM bans WHERE user = ?", user.ID).Scan(&banCount)
	if banCount > 0 {
		if len(sidebar.User.Role.Organization) > 0 {
			sidebar.User.Role.Organization = "Banned<br>" + sidebar.User.Role.Organization
		} else {
			sidebar.User.Role.Organization = "Banned"
		}
	}

	db.QueryRow("SELECT COUNT(*) FROM posts WHERE created_by = ? AND is_rm = 0", user.ID).Scan(&sidebar.Profile.PostCount)
	db.QueryRow("SELECT COUNT(*) FROM comments WHERE created_by = ? AND is_rm = 0", user.ID).Scan(&sidebar.Profile.CommentCount)
	db.QueryRow("SELECT COUNT(*) FROM yeahs WHERE yeah_by = ?", user.ID).Scan(&sidebar.Profile.YeahCount)

	err := db.QueryRow("SELECT image FROM posts WHERE id = ?", sidebar.Profile.FavoritePostID).Scan(&sidebar.Profile.FavoritePostImage)

	favorite_rows, err := db.Query("SELECT communities.id, title, icon FROM communities LEFT JOIN community_favorites ON communities.id = community WHERE favorite_by = ? AND rm = 0 ORDER BY community_favorites.id DESC LIMIT 10", user.ID)
	if err != nil {
		fmt.Println("error while getting favorite communities")
		fmt.Println(err.Error())
		return sidebar
	}

	var favorites []community
	for favorite_rows.Next() {
		var row = community{}

		err := favorite_rows.Scan(&row.ID, &row.Title, &row.Icon)
		if err != nil {
			fmt.Println("error while scanning favorite communities")
			fmt.Println(err.Error())
		}

		favorites = append(favorites, row)
	}
	favorite_rows.Close()
	sidebar.FavoriteCommunities = favorites
	return sidebar
}

// Set friend/following/follower counts for sidebars.
func setupSidebarStatus(userID int) (int, int, int) {
	friendCount, followingCount, followerCount := 0, 0, 0
	if userID != 0 {
		db.QueryRow("SELECT COUNT(*) FROM friendships WHERE source = ? OR target = ?", userID, userID).Scan(&friendCount)
		db.QueryRow("SELECT COUNT(*) FROM follows WHERE follow_by = ?", userID).Scan(&followingCount)
		db.QueryRow("SELECT COUNT(*) FROM follows WHERE follow_to = ?", userID).Scan(&followerCount)
	}
	return friendCount, followingCount, followerCount
}

// Cut a string off at 200 characters if needed, and parse Markdown and later emotes.
func parseBody(body string, cutoff bool, parseMarkdown bool) template.HTML {
	// Cut off at 200 characters if cutoff is set.
	if cutoff && utf8.RuneCountInString(body) > 203 {
		runes := []rune(body) // What is this, fucking RuneScape!?
		body = string(runes[0:200]) + "..."
	}

	// Parse markdown and sanitize HTML.
	if parseMarkdown {
		body = strings.Replace(body, "<3", "\\<3", -1)
		renderer := blackfriday.NewHTMLRenderer(blackfriday.HTMLRendererParameters{Flags: 2 | 4 | 128, })
		bodyTemp := blackfriday.Run([]byte(body), blackfriday.WithRenderer(renderer))
		if len(bodyTemp) >= 7 {
			rune2 := []rune(string(bodyTemp))
			body = string(rune2[:len(rune2)-1])
		} else {
			body = string(bodyTemp)
		}
	}
	body = bluemonday.UGCPolicy().Sanitize(body)

	// Parse emotes.
	matches := emotes.FindAllStringSubmatch(body, settings.EmoteLimit)
	for _, match := range matches {
		var image sql.NullString
		db.QueryRow("SELECT image FROM emotes WHERE name = ?", match[1]).Scan(&image)
		if image.Valid {
			if len(image.String) > 0 {
				body = strings.Replace(body, match[0], "<img title=\"꞉"+match[1]+"꞉\" src=\""+image.String+"\">", 1)
			} else {
				body = strings.Replace(body, match[0], "", 1)
			}
		}
	}

	// Return the output.
	return template.HTML(body)
}

// Parse a body with parseBody(), and then replace line breaks with <br> elements.
func parseBodyWithLineBreaks(body string, cutoff bool, parseMarkdown bool) template.HTML {
	bodyHTML := parseBody(body, cutoff, parseMarkdown)
	body = strings.Replace(string(bodyHTML), "\n", "<br>", -1)
	return template.HTML(body)
}

// Cut a string off at 200 characters.
func parseCutoff(body template.HTML) template.HTML {
	return body
}

// Cut a string off at 15 characters. Used for notifications and the "View _____'s post for this comment" bar thingy at the top of the comments page.
func parsePreview(body string, postType int, isRM bool) string {
	if isRM {
		body = "deleted"
	} else if len(body) == 0 {
		body = "empty"
	} else if postType == 1 {
		body = "handwritten"
	} else if utf8.RuneCountInString(body) > 18 {
		runes := []rune(body)
		body = string(runes[0:15]) + "..."
	}
	return body
}

// Really minimal function to check if the user viewing the page can give a Yeah to a certain post.
func checkIfCanYeah(currentUser user, createdBy int) bool {
	if len(currentUser.Username) == 0 {
		return false
	}
	if createdBy == currentUser.ID {
		return false
	}
	if checkIfEitherBlocked(currentUser.ID, createdBy) {
		return false
	}
	return true
}

// Generate the name of a group chat from an array of user nicknames.
func getGroupName(users []string) string {
	groupName := "Group chat with "
	if len(users) < 1 {
		groupName += "yourself"
	} else if len(users) == 1 {
		groupName += users[0]
	} else if len(users) == 2 {
		groupName += users[0] + " and " + users[1]
	} else {
		for i := 0; i < len(users)-2; i++ {
			groupName += users[i] + ", "
		}
		groupName += users[len(users)-2] + " and " + users[len(users)-1]
	}
	return groupName
}

// Fetch the site's settings from a config.json file.
func getSettings() config {
	var settings config
	settingsJSON, err := ioutil.ReadFile("config.json")
	if err != nil {
		fmt.Println(err.Error())
		return settings
	}
	err = json.Unmarshal(settingsJSON, &settings)
	if err != nil {
		fmt.Println(err.Error())
	}
	settings.ReportReasons = append([]reportReason{{
		Name:         "Spoiler",
		Message:      "Your post contained spoilers, so it was removed.",
		Enabled:      false,
		BodyRequired: true,
	}}, settings.ReportReasons...)
	return settings
}

// Generate a login token for autoauth.
func generateLoginToken() string {
	const letterBytes = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz1234567890"
	const (
		letterIdxBits = 6
		letterIdxMask = 1<<letterIdxBits - 1
		letterIdxMax  = 63 / letterIdxBits
	)
	src := rand.NewSource(time.Now().UnixNano())
	b := make([]byte, 16)

	for i, cache, remain := 15, src.Int63(), letterIdxMax; i >= 0; {
		if remain == 0 {
			cache, remain = src.Int63(), letterIdxMax
		}
		if idx := int(cache & letterIdxMask); idx < len(letterBytes) {
			b[i] = letterBytes[idx]
			i--
		}
		cache >>= letterIdxBits
		remain--
	}
	return string(b)
}

// Check if a user is blocking another user.
func checkIfBlocked(source int, target int) bool {
	var isBlocked bool
	err := db.QueryRow("SELECT COUNT(*) FROM blocks WHERE source = ? AND target = ?", source, target).Scan(&isBlocked)
	if err != nil {
		fmt.Println("error while getting blocks")
		fmt.Println(err.Error())
	}
	return isBlocked
}

// Check if a user is blocking another user, or vice versa.
func checkIfEitherBlocked(source int, target int) bool {
	var isBlocked bool
	err := db.QueryRow("SELECT COUNT(*) FROM blocks WHERE (source = ? AND target = ?) OR (source = ? AND target = ?)", source, target, target, source).Scan(&isBlocked)
	if err != nil {
		fmt.Println("error while checking if either ballcoks")
		fmt.Println(err.Error())
	}
	return isBlocked
}

// Render a user's avatar as a Mii URL with an emotion or return it if it's not a Mii.
func getAvatar(avatar string, hasMii bool, feeling int) string {
	const url = "https://mii-secure.cdn.nintendo.net/%s_%s_face.png"
	if len(avatar) == 0 {
		return "/assets/img/anonymous.png"
	}
	if hasMii {
		switch feeling {
		case 1, 8:
			return fmt.Sprintf(url, avatar, "happy")
		case 2, 7:
			return fmt.Sprintf(url, avatar, "like")
		case 3, 6:
			return fmt.Sprintf(url, avatar, "surprised")
		case 4:
			return fmt.Sprintf(url, avatar, "frustrated")
		case 5:
			return fmt.Sprintf(url, avatar, "puzzled")
		default:
			return fmt.Sprintf(url, avatar, "normal")
		}
	}
	return avatar
}

// Get the database values necessary for showing a comment preview.
func getCommentPreview(postID int, currentUser user) comment {
	var commentPreview comment
	var timestamp time.Time
	var editedAt time.Time
	var role int

	db.QueryRow("SELECT comments.id, created_at, edited_at, feeling, body, post_type, username, nickname, avatar, has_mh, online, hide_online, color, role FROM comments INNER JOIN users ON users.id = created_by WHERE post = ? AND is_rm = 0 AND is_rm_by_admin = 0 AND is_spoiler = 0 AND (users.id NOT IN (SELECT if(source = ?, target, source) FROM blocks WHERE (source = ? AND target = users.id) OR (source = users.id AND target = ?)) OR ? > 0) AND IF(created_by = ?, true, LOWER(body) NOT REGEXP LOWER(?)) ORDER BY comments.id DESC LIMIT 1", postID, currentUser.ID, currentUser.ID, currentUser.ID, currentUser.Level, currentUser.ID, escapeForbiddenKeywords(currentUser.ForbiddenKeywords)).Scan(&commentPreview.ID, &timestamp, &editedAt, &commentPreview.Feeling, &commentPreview.BodyText, &commentPreview.PostType, &commentPreview.CommenterUsername, &commentPreview.CommenterNickname, &commentPreview.CommenterIcon, &commentPreview.CommenterHasMii, &commentPreview.CommenterOnline, &commentPreview.CommenterHideOnline, &commentPreview.CommenterColor, &role)
	commentPreview.CommenterIcon = getAvatar(commentPreview.CommenterIcon, commentPreview.CommenterHasMii, commentPreview.Feeling)
	if role > 0 {
		commentPreview.CommenterRoleImage = getRoleImage(role)
	}
	commentPreview.CreatedAt = humanTiming(timestamp, currentUser.Timezone)
	commentPreview.CreatedAtUnix = timestamp.Unix()
	if editedAt.Sub(timestamp).Minutes() > 5 {
		commentPreview.EditedAt = humanTiming(editedAt, currentUser.Timezone)
		commentPreview.EditedAtUnix = editedAt.Unix()
	}
	commentPreview.Body = parseBody(commentPreview.BodyText, false, true)

	return commentPreview
}

// Check if a string violates a user's forbidden keywords.
func inForbiddenKeywords(text string, userID int) bool {
	var forbiddenKeywords string
	err := db.QueryRow("SELECT forbidden_keywords FROM users WHERE id = ?", userID).Scan(&forbiddenKeywords)
	if err != nil {
		fmt.Println("error while getting forbiden keywrod")
		fmt.Println(err)
	}
	isMatch, err := regexp.MatchString(escapeForbiddenKeywords(forbiddenKeywords), text)
	if err != nil {
		fmt.Println("error while dying")
		fmt.Println(err)
	}
	return isMatch
}

// Get the current hostname.
func getHostname(host string) string {
	hostname := "http"
	if settings.SSL.Enabled {
		hostname += "s"
	}
	hostname += "://" + host
	if (settings.Port == ":80" && settings.SSL.Enabled) || (settings.Port == ":443" && !settings.SSL.Enabled) || (settings.Port != ":80" && settings.Port != ":443") {
		hostname += settings.Port
	}

	return hostname
}

// Get the current user's IP.
func getIP(r *http.Request) string {
	if settings.Proxy && len(r.Header.Get("X-Forwarded-For")) > 0 { // Proxy sites like Cloudflare mask the IP, so grab that from the headers... if it's set in the settings, that is; otherwise, people could fake this and we'd have an impersonation exploit on our hands. (Looking at you, Seth)
		ips := strings.Split(r.Header.Get("X-Forwarded-For"), ", ")
		return ips[0] + settings.Port
	} else {
		return r.RemoteAddr
	}
}

// Get a poll from an ID.
func getPoll(pollID int, userID int) poll {
	var newPoll poll
	option_rows, err := db.Query("SELECT options.id, name FROM options WHERE post = ?", pollID)
	if err != nil {
		fmt.Println("could not get poll")
		fmt.Println(err.Error())
		return newPoll
	}
	for option_rows.Next() {
		var row = option{}
		option_rows.Scan(&row.ID, &row.Name)
		user_rows, err := db.Query("SELECT users.id FROM votes LEFT JOIN users ON users.id = user WHERE poll = ? AND option_id = ?", pollID, row.ID)
		if err != nil {
			fmt.Println("could not die")
			fmt.Println(err.Error())
			return newPoll
		}
		for user_rows.Next() { // OPTIMIZE THIS!!!
			var currentID int
			user_rows.Scan(&currentID)
			if currentID == userID {
				row.Selected = true
				newPoll.Selected = true
			}

			row.Votes = row.Votes + 1
		}
		user_rows.Close()

		newPoll.Options = append(newPoll.Options, row)
	}
	option_rows.Close()

	for _, row := range newPoll.Options {
		newPoll.Votes = newPoll.Votes + row.Votes
	}
	for i, row := range newPoll.Options {
		if row.Votes > 0 {
			newPoll.Options[i].Percentage = math.Round(row.Votes / newPoll.Votes * 100)
		} else {
			row.Percentage = 0
		}
	}

	newPoll.ID = pollID
	return newPoll
}

// Get the image of a role from its ID.
func getRoleImage(roleID int) string {
	var image string
	err := db.QueryRow("SELECT image FROM roles WHERE id = ?", roleID).Scan(&image)
	if err != nil {
		fmt.Println("getROle Image fail")
		fmt.Println(err.Error())
	}
	return image
}

// Get the image and organization of a role from its ID.
func getRoleImageAndOrganization(roleID int) (string, string) {
	var image string
	var organization string
	err := db.QueryRow("SELECT image, organization FROM roles WHERE id = ?", roleID).Scan(&image, &organization)
	if err != nil {
		fmt.Println("can't do that")
		fmt.Println(err.Error())
	}
	return image, organization
}

// Get a user's timezone from their IP.
func getTimezone(ip string) string {
	if isGeoIPEnabled == false {
		return settings.DefaultTimezone
	}

	parsedHost, _, _ := net.SplitHostPort(ip)
	parsedIP := net.ParseIP(parsedHost)
	if parsedIP == nil {
		fmt.Println("parsedIP was nil")
		return settings.DefaultTimezone
	}

	city, err := geoip.City(parsedIP)
	if err != nil {
		fmt.Println("no city")
		fmt.Println(err.Error())
		return settings.DefaultTimezone
	}

	if len(city.Location.TimeZone) > 0 {
		return city.Location.TimeZone
	} else {
		return settings.DefaultTimezone
	}
}
