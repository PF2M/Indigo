////////////////////////
//                    //
//       Indigo       //
// The Miiverse clone //
// that will end all  //
//   other Miiverse   //
//  clones, for real  //
//     this time.     //
//                    //
//  Lead Devs: PF2M,  //
// Seth/EnergeticBark //
//                    //
//  Developers: Ben,  //
// triangles.py, jod, //
// & Chance/SRGNation //
//                    //
//  Artwork: Spicy &  //
//  Inverse & Gnarly  //
//                    //
//   Marketing: Pip   //
//                    //
//  Testing: Mippy ♥  //
//                    //
// https://github.com //
//    /PF2M/Indigo    //
//                    //
////////////////////////

package main

// Import dependencies.
import (
	// Internals
	"database/sql"
	"encoding/json"
	"html/template"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"

	// "user" is already defined in types
	osUser "os/user"
	"strconv"

	"regexp"

	// Externals
	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/csrf"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/oschwald/geoip2-golang"
	"github.com/russross/blackfriday/v2"
	"github.com/NYTimes/gziphandler"
)

// Initialize some variables.
var db *sql.DB
var err error
var clients = make(map[*websocket.Conn]*wsSession)
var settings config
var admin adminConfig
var youtube *regexp.Regexp
var spotify *regexp.Regexp
var soundcloud *regexp.Regexp
var symbols *regexp.Regexp
var emotes *regexp.Regexp
var renderer *blackfriday.HTMLRenderer
var geoip *geoip2.Reader
var isGeoIPEnabled bool

// Configure the upgrader.
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// Todo: Add to this if necessary
		return true
	},
	EnableCompression: true,
}

// Define the templates.
var templates = template.Must(template.ParseFiles("views/auth/login.html", "views/auth/signup.html", "views/index.html", "views/elements/header.html", "views/elements/footer.html", "views/communities.html", "views/post.html", "views/comment.html", "views/elements/render_comment_preview.html", "views/elements/create_comment.html", "views/elements/profile_sidebar.html", "views/user.html", "views/user_list.html", "views/notifications.html", "views/user_posts.html", "views/elements/general_sidebar.html", "views/help/faq.html", "views/help/rules.html", "views/help/legal.html", "views/help/contact.html", "views/error.html", "views/friend_requests.html", "views/messages.html", "views/conversation.html", "views/elements/render_message.html", "views/activity_loading.html", "views/activity.html", "views/elements/render_post.html", "views/profile_settings.html", "views/search.html", "views/auth/ban.html", "views/all_communities.html", "views/blocked.html", "views/create_group.html", "views/auth/reset.html", "views/elements/render_user_post.html", "views/elements/render_comment.html", "views/all_comments.html", "views/elements/poll.html", "views/recent_communities.html", "views/admin/dashboard.html", "views/admin/manage.html", "views/admin/settings.html", "views/admin/audit_logs.html"))

// Redirect HTTP requests to HTTPS if properly configured.
func redirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "https://"+r.Host+r.URL.Path, http.StatusTemporaryRedirect)
}

// Now let's start the main function!
func main() {
	// Fetch the site's settings from JSON files.
	settings = getSettings()
	adminJSON, err := ioutil.ReadFile("admin.json")
	if err != nil {
		log.Fatal(err)
	}
	err = json.Unmarshal(adminJSON, &admin)
	if err != nil {
		log.Fatal(err)
	}

	// Connect to the database.
	db, err = sql.Open("mysql", settings.DB.Username + ":" + settings.DB.Password + "@tcp(" + settings.DB.Host + ")/" + settings.DB.Name + "?parseTime=true&loc=US%2FEastern&charset=utf8mb4,utf8")
	if err != nil {
		log.Printf("[err]: unable to connect to the database...\n")
		log.Printf("       %v\n", err)
		os.Exit(1)
	}

	// Ping the database to make sure we connected properly.
	err = db.Ping()
	if err != nil {
		log.Printf("[err]: unable to ping the database...\n")
		log.Printf("       %v\n", err)
		os.Exit(1)
	}
	_, err = db.Exec("SET CHARACTER SET utf8mb4")
	if err != nil {
		log.Printf("[err]: unable to set the character set...\n")
		log.Printf("       %v\n", err)
		os.Exit(1)
	}
	_, err = db.Exec("SET collation_connection = utf8mb4_bin")
	if err != nil {
		log.Printf("[err]: unable to set the connection collation...\n")
		log.Printf("       %v\n", err)
		os.Exit(1)
	}

	// Initialize some regex.
	youtube, _ = regexp.Compile("(?:youtube\\.com\\/\\S*(?:(?:\\/e(?:mbed))?\\/|watch\\/?\\?(?:\\S*?&?v\\=))|youtu\\.be\\/)([a-zA-Z0-9_-]{6,11})")
	spotify, _ = regexp.Compile("(?:embed\\.|open\\.)(?:spotify\\.com\\/)(?:track\\/|\\?uri=spotify:track:)((\\w|-){22})")
	soundcloud, _ = regexp.Compile("(soundcloud\\.com|snd\\.sc)\\/(.*)")
	symbols, _ = regexp.Compile("(\\|\\\\|`|\\*|{|}|\\[|\\](|)|\\+|-|!|_|>|\\n|&|:|<)")
	emotes, err = regexp.Compile(":([^ :]+):")
	if err != nil {
		log.Fatal(err)
	}

	// Initialize Markdown renderer.
	renderer = blackfriday.NewHTMLRenderer(blackfriday.HTMLRendererParameters{
		Flags: 2 | 4 | 128,
	})

	// Initialize GeoIP if a database is present.
	isGeoIPEnabled = false
	if _, err = os.Stat("geoip.mmdb"); err == nil {
		geoip, err = geoip2.Open("geoip.mmdb")
		if err != nil {
			log.Fatal(err)
		}
		defer geoip.Close()
		isGeoIPEnabled = true
	}

	// Wipe the online statuses of all the users and delete all session keys (necessary after crashes, shutdowns, etc.)
	db.QueryRow("UPDATE users SET online = 0").Scan()
	db.QueryRow("TRUNCATE TABLE sessions").Scan()

	// Close the database connection after this function exits.
	defer db.Close()

	// Set up CSRF.
	CSRF := csrf.Protect([]byte(settings.CSRFSecret), csrf.FieldName("csrfmiddlewaretoken"), csrf.Path("/"), csrf.Secure(settings.SSL.Enabled))

	// Initialize routes.
	r := mux.NewRouter()

	// Index route.
	r.HandleFunc("/", index).Methods("GET")

	// Auth routes.
	r.HandleFunc("/signup", signup).Methods("GET", "POST")
	r.HandleFunc("/login", login).Methods("GET", "POST")
	r.HandleFunc("/logout", logout).Methods("POST")
	r.HandleFunc("/reset", resetPassword).Methods("GET", "POST").Queries("token", "{token}")
	r.HandleFunc("/reset", showResetPassword).Methods("GET", "POST")

	// User routes.
	r.HandleFunc("/users", showUserSearch).Methods("GET").Queries("query", "{username}")
	r.HandleFunc("/users/{username}", showUser).Methods("GET")
	r.HandleFunc("/users/{username}/posts", showUserPosts).Methods("GET")
	r.HandleFunc("/users/{username}/comments", showUserComments).Methods("GET")
	r.HandleFunc("/users/{username}/yeahs", showUserYeahs).Methods("GET")
	r.HandleFunc("/users/{username}/friends", showFriends).Methods("GET")
	r.HandleFunc("/users/{username}/following", showFollowing).Methods("GET")
	r.HandleFunc("/users/{username}/followers", showFollowers).Methods("GET")
	r.HandleFunc("/users/{username}/favorites", showFavorites).Methods("GET")
	r.HandleFunc("/users/{username}/friend_new", newFriendRequest).Methods("POST")
	r.HandleFunc("/users/{username}/friend_accept", acceptFriendRequest).Methods("POST")
	r.HandleFunc("/users/{username}/friend_reject", rejectFriendRequest).Methods("POST")
	r.HandleFunc("/users/{username}/friend_cancel", cancelFriendRequest).Methods("POST")
	r.HandleFunc("/users/{username}/friend_delete", deleteFriend).Methods("POST")
	r.HandleFunc("/users/{username}/follow", createFollow).Methods("POST")
	r.HandleFunc("/users/{username}/unfollow", deleteFollow).Methods("POST")
	r.HandleFunc("/users/{username}/violators", reportUser).Methods("POST")
	r.HandleFunc("/users/{username}/block", blockUser).Methods("POST")
	r.HandleFunc("/users/{username}/unblock", unblockUser).Methods("POST")

	// Post routes.
	r.HandleFunc("/posts/{id:[0-9]+}", showPost).Methods("GET")
	r.HandleFunc("/posts/{id:[0-9]+}/yeah", createPostYeah).Methods("POST")
	r.HandleFunc("/posts/{id:[0-9]+}/yeahu", deletePostYeah).Methods("POST")
	r.HandleFunc("/posts/{id:[0-9]+}/comments", showAllComments).Methods("GET")
	r.HandleFunc("/posts/{id:[0-9]+}/comments", createComment).Methods("POST")
	r.HandleFunc("/posts/{id:[0-9]+}/favorite", favoritePost).Methods("POST")
	r.HandleFunc("/posts/{id:[0-9]+}/unfavorite", unfavoritePost).Methods("POST")
	r.HandleFunc("/posts/{id:[0-9]+}/violations", reportPost).Methods("POST")
	r.HandleFunc("/posts/{id:[0-9]+}/vote", voteOnPoll).Methods("POST")
	r.HandleFunc("/posts/{id:[0-9]+}/edit", editPost).Methods("POST")
	r.HandleFunc("/posts/{id:[0-9]+}/delete", deletePost).Methods("POST")

	// Comment routes.
	r.HandleFunc("/comments/{id:[0-9]+}", showComment).Methods("GET")
	r.HandleFunc("/comments/{id:[0-9]+}/yeah", createCommentYeah).Methods("POST")
	r.HandleFunc("/comments/{id:[0-9]+}/yeahu", deleteCommentYeah).Methods("POST")
	r.HandleFunc("/comments/{id:[0-9]+}/violations", reportComment).Methods("POST")
	r.HandleFunc("/comments/{id:[0-9]+}/edit", editComment).Methods("POST")
	r.HandleFunc("/comments/{id:[0-9]+}/delete", deleteComment).Methods("POST")

	// Community routes.
	r.HandleFunc("/communities/all", showAllCommunities).Methods("GET")
	r.HandleFunc("/communities/recent", showRecentCommunities).Methods("GET")
	r.HandleFunc("/communities/search", showCommunitySearch).Methods("GET").Queries("query", "{search}")
	r.HandleFunc("/communities/{id:[0-9]+}", showCommunity).Methods("GET")
	r.HandleFunc("/communities/{id:[0-9]+}/hot", showPopularPosts).Methods("GET")
	r.HandleFunc("/communities/{id:[0-9]+}/posts", createPost).Methods("POST")
	r.HandleFunc("/communities/{id:[0-9]+}/favorite", addCommunityFavorite).Methods("POST")
	r.HandleFunc("/communities/{id:[0-9]+}/unfavorite", deleteCommunityFavorite).Methods("POST")

	// Activiy Feed route.
	r.HandleFunc("/activity", showActivityFeed).Methods("GET")

	// Message routes.
	r.HandleFunc("/messages", showMessages).Methods("GET")
	r.HandleFunc("/messages", sendMessage).Methods("POST")
	r.HandleFunc("/messages/{id:[0-9]+}/delete", deleteMessage).Methods("POST")
	r.HandleFunc("/messages/{username}", showConversation).Methods("GET")
	r.HandleFunc("/conversations/{id:[0-9]+}", showGroupChat).Methods("GET")
	r.HandleFunc("/conversations/create", showCreateGroupChat).Methods("GET")
	r.HandleFunc("/conversations/create", createGroupChat).Methods("POST")
	r.HandleFunc("/conversations/{id:[0-9]+}/edit", showEditGroupChat).Methods("GET")
	r.HandleFunc("/conversations/{id:[0-9]+}/edit", editGroupChat).Methods("POST")
	r.HandleFunc("/conversations/{id:[0-9]+}/leave", leaveGroupChat).Methods("POST")
	r.HandleFunc("/conversations/{id:[0-9]+}/delete", deleteGroupChat).Methods("POST")

	// Notification routes.
	r.HandleFunc("/check_update.json", getNotificationCounts).Methods("GET")
	r.HandleFunc("/notifications", showNotifications).Methods("GET")
	r.HandleFunc("/notifications/friend_requests", showFriendRequests).Methods("GET")

	// Settings routes.
	r.HandleFunc("/settings/profile", showProfileSettings).Methods("GET")
	r.HandleFunc("/settings/profile", editProfileSettings).Methods("POST")
	r.HandleFunc("/region", getRegion).Methods("POST")
	r.HandleFunc("/miis", getMii).Methods("POST")
	r.HandleFunc("/migrate/{id:[0-9]+}", migratePosts).Methods("POST")
	r.HandleFunc("/rollback/{id:[0-9]+}", rollbackImport).Methods("POST")
	r.HandleFunc("/settings/account", showAccountSettings).Methods("GET")
	r.HandleFunc("/settings/account", editAccountSettings).Methods("POST")
	r.HandleFunc("/blocked", showBlocked).Methods("GET")

	// Help page routes.
	r.HandleFunc("/help/rules", showRulesPage).Methods("GET")
	r.HandleFunc("/help/faq", showFAQPage).Methods("GET")
	r.HandleFunc("/help/legal", showLegalPage).Methods("GET")
	r.HandleFunc("/help/contact", showContactPage).Methods("GET")

	// Image upload route.
	r.HandleFunc("/upload", uploadImage).Methods("POST")

	// Admin routes.
	r.HandleFunc("/admin", showAdminDashboard).Methods("GET")
	r.HandleFunc("/reports/{id:[0-9]+}/ignore", reportIgnore).Methods("POST")
	r.HandleFunc("/admin/manage", showAdminManagerList).Methods("GET")
	r.HandleFunc("/admin/manage/bantemp", adminBanUser).Methods("POST")
	r.HandleFunc("/admin/manage/unbantemp", adminUnbanUser).Methods("POST")
	//r.HandleFunc("/admin/manage/{table}", showAdminManager).Methods("GET")
	//r.HandleFunc("/admin/manage/{table}/{id:[0-9]+}", showAdminEditor).Methods("GET", "POST")
	r.HandleFunc("/admin/settings", showAdminSettings).Methods("GET", "POST")
	r.HandleFunc("/admin/audit_log", showAdminAuditLog).Methods("GET")

	// Websocket route.
	r.HandleFunc("/ws", handleConnections).Methods("GET")

	// Add a 404 page.
	r.NotFoundHandler = http.HandlerFunc(handle404)

	// Serve static assets.
	r.PathPrefix("/assets/").Handler(http.StripPrefix("/assets/", http.FileServer(http.Dir("assets"))))
	// serve images as /images even though this can be changed
        r.PathPrefix("/images/").Handler(http.StripPrefix("/images/", http.FileServer(http.Dir("images"))))

	var handler http.Handler = r

	if !settings.CSRFProtectDisable {
		handler = CSRF(r)
	}
	if settings.GzipEnabled {
		handler = gziphandler.GzipHandler(handler)
	}

	// Tell the http server to handle routing with the router we just made.
	http.Handle("/", handler)
	// Tell the person who started this that we are starting the server.
	log.Printf("listening on " + settings.Port)

	// Start the server.
	if settings.ListenSocket {
		// remove tha socket first or else
		os.Remove(settings.Port)

		unixListener, err := net.Listen("unix", settings.Port)
		if err != nil {
			log.Fatal("cannot listen on unix socket: ", err)
		}

		// set socket owner but only if the value is not blank
		if settings.SocketOwner != "" {
			socketUser, err := osUser.Lookup(settings.SocketOwner)
			if err != nil {
				log.Fatal("could not look up user so that we can change the owner of the unix socket so that we can listen on it:\n", err)
			}
			// should probably handle errors here
			uidInt, _ := strconv.Atoi(socketUser.Uid)
			gidInt, _ := strconv.Atoi(socketUser.Gid)
			err = os.Chown(settings.Port, uidInt, gidInt)
			if err != nil {
				log.Fatal("could not change socket owner", err)
			}
		}

		err = http.Serve(unixListener, nil) // Just serve HTTP requests.
		if err != nil {
			log.Fatal(err)
		}
	} else {
		if settings.SSL.Enabled && settings.Port != ":80" {
			go http.ListenAndServe(":80", http.HandlerFunc(redirect)) // Redirect HTTP requests to the HTTPS site.
			err = http.ListenAndServeTLS(settings.Port, settings.SSL.Certificate, settings.SSL.Key, nil)
			if err != nil {
				log.Fatal(err)
			}
		} else {
			log.Fatal(http.ListenAndServe(settings.Port, nil))
		}
	}
}
