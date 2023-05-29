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
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"

	// "user" is already defined in types
	osUser "os/user"
	"strconv"

	"regexp"

	// Externals
	"github.com/NYTimes/gziphandler"
	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/csrf"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/oschwald/geoip2-golang"
	"github.com/russross/blackfriday/v2"
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
var templates *template.Template

// Redirect HTTP requests to HTTPS if properly configured.
func redirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "https://"+r.Host+r.URL.Path, http.StatusTemporaryRedirect)
}

// Now let's start the main function!
func main() {
	// Fetch the site's settings from JSON files.
	settings = getSettings()
	adminJSON, err := os.ReadFile("admin.json")
	if err != nil {
		log.Fatal(err)
	}
	err = json.Unmarshal(adminJSON, &admin)
	if err != nil {
		log.Fatal(err)
	}

	// Connect to the database.
	db, err = sql.Open("mysql", settings.DB.Username+":"+settings.DB.Password+"@tcp("+settings.DB.Host+")/"+settings.DB.Name+"?parseTime=true&loc=US%2FEastern&charset=utf8mb4,utf8")
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
	youtube, _ = regexp.Compile("(?:youtube\\.com/\\S*(?:(?:/e(?:mbed))?/|watch/?\\?(?:\\S*?&?v=))|youtu\\.be/)([a-zA-Z0-9_-]{6,11})")
	spotify, _ = regexp.Compile("(?:embed\\.|open\\.)(?:spotify\\.com/)(?:track/|\\?uri=spotify:track:)((\\w|-){22})")
	soundcloud, _ = regexp.Compile("(soundcloud\\.com|snd\\.sc)(.*)")
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

	// initialize the templates by parsing everything from the views directory recursively
	var tmplFiles []string
	err = filepath.Walk("views", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// exclude non-html files
		if !info.IsDir() && filepath.Ext(path) == ".html" {
			// feel free to instead make this directly build the template
			tmplFiles = append(tmplFiles, path)
		}
		return nil
	})
	if err != nil {
		log.Fatal("could not add or find templates (they are stored in views, is this accessible?): ", err)
	}

	templates = template.Must(template.ParseFiles(tmplFiles...))

	// make the directory for the local image provider if it doesn't exist
	if settings.ImageHost.Provider == "local" {
		// check if the error is specifically os.IsNotExist
		if _, err := os.Stat(settings.ImageHost.ImageEndpoint); os.IsNotExist(err) {
			// should make it in this working directory
			err = os.MkdirAll(settings.ImageHost.ImageEndpoint, 0755)
			if err != nil {
				log.Println("could not make \""+settings.ImageHost.ImageEndpoint+"\" directory for local image host:", err)
			}
		}
	}

	// Set up CSRF.
	CSRF := csrf.Protect([]byte(settings.CSRFSecret), csrf.FieldName("csrfmiddlewaretoken"), csrf.Path("/"), csrf.Secure(settings.SSL.Enabled))

	// Initialize routes.
	r := mux.NewRouter()

	// functions that don't useLogin or requireLogin,
	// they don't necessarily not access the user
	// but just do it independently, not utilizing the CurrentUser

	// Index route.
	r.HandleFunc("/", useLogin(index)).Methods("GET")

	// Auth routes.
	r.HandleFunc("/signup", signup).Methods("GET", "POST")
	r.HandleFunc("/login", login).Methods("GET", "POST")
	r.HandleFunc("/logout", logout).Methods("POST")
	r.HandleFunc("/reset", useLogin(resetPassword)).Methods("GET", "POST").Queries("token", "{token}")
	r.HandleFunc("/reset", useLogin(showResetPassword)).Methods("GET", "POST")

	// User routes.
	r.HandleFunc("/users", requireLogin(showUserSearch)).Methods("GET").Queries("query", "{username}")
	r.HandleFunc("/users/{username}", useLogin(showUser)).Methods("GET")
	r.HandleFunc("/users/{username}/posts", useLogin(showUserPosts)).Methods("GET")
	r.HandleFunc("/users/{username}/comments", useLogin(showUserComments)).Methods("GET")
	r.HandleFunc("/users/{username}/yeahs", useLogin(showUserYeahs)).Methods("GET")
	r.HandleFunc("/users/{username}/friends", useLogin(showFriends)).Methods("GET")
	r.HandleFunc("/users/{username}/following", useLogin(showFollowing)).Methods("GET")
	r.HandleFunc("/users/{username}/followers", useLogin(showFollowers)).Methods("GET")
	r.HandleFunc("/users/{username}/favorites", useLogin(showFavorites)).Methods("GET")
	r.HandleFunc("/users/{username}/friend_new", requireLogin(newFriendRequest)).Methods("POST")
	r.HandleFunc("/users/{username}/friend_accept", requireLogin(acceptFriendRequest)).Methods("POST")
	r.HandleFunc("/users/{username}/friend_reject", requireLogin(rejectFriendRequest)).Methods("POST")
	r.HandleFunc("/users/{username}/friend_cancel", requireLogin(cancelFriendRequest)).Methods("POST")
	r.HandleFunc("/users/{username}/friend_delete", requireLogin(deleteFriend)).Methods("POST")
	r.HandleFunc("/users/{username}/follow", requireLogin(createFollow)).Methods("POST")
	r.HandleFunc("/users/{username}/unfollow", requireLogin(deleteFollow)).Methods("POST")
	r.HandleFunc("/users/{username}/violators", requireLogin(reportUser)).Methods("POST")
	r.HandleFunc("/users/{username}/block", requireLogin(blockUser)).Methods("POST")
	r.HandleFunc("/users/{username}/unblock", requireLogin(unblockUser)).Methods("POST")

	// Post routes.
	r.HandleFunc("/posts/{id:[0-9]+}", useLogin(showPost)).Methods("GET")
	r.HandleFunc("/posts/{id:[0-9]+}/yeah", requireLogin(createPostYeah)).Methods("POST")
	r.HandleFunc("/posts/{id:[0-9]+}/yeahu", requireLogin(deletePostYeah)).Methods("POST")
	r.HandleFunc("/posts/{id:[0-9]+}/comments", useLogin(showAllComments)).Methods("GET")
	r.HandleFunc("/posts/{id:[0-9]+}/comments", requireLogin(createComment)).Methods("POST")
	r.HandleFunc("/posts/{id:[0-9]+}/favorite", requireLogin(favoritePost)).Methods("POST")
	r.HandleFunc("/posts/{id:[0-9]+}/unfavorite", requireLogin(unfavoritePost)).Methods("POST")
	r.HandleFunc("/posts/{id:[0-9]+}/violations", requireLogin(reportPost)).Methods("POST")
	r.HandleFunc("/posts/{id:[0-9]+}/vote", requireLogin(voteOnPoll)).Methods("POST")
	r.HandleFunc("/posts/{id:[0-9]+}/edit", requireLogin(editPost)).Methods("POST")
	r.HandleFunc("/posts/{id:[0-9]+}/delete", requireLogin(deletePost)).Methods("POST")

	// Comment routes.
	r.HandleFunc("/comments/{id:[0-9]+}", useLogin(showComment)).Methods("GET")
	r.HandleFunc("/comments/{id:[0-9]+}/yeah", requireLogin(createCommentYeah)).Methods("POST")
	r.HandleFunc("/comments/{id:[0-9]+}/yeahu", requireLogin(deleteCommentYeah)).Methods("POST")
	r.HandleFunc("/comments/{id:[0-9]+}/violations", requireLogin(reportComment)).Methods("POST")
	r.HandleFunc("/comments/{id:[0-9]+}/edit", requireLogin(editComment)).Methods("POST")
	r.HandleFunc("/comments/{id:[0-9]+}/delete", requireLogin(deleteComment)).Methods("POST")

	// Community routes.
	r.HandleFunc("/communities/all", useLogin(showAllCommunities)).Methods("GET")
	r.HandleFunc("/communities/recent", requireLogin(showRecentCommunities)).Methods("GET")
	r.HandleFunc("/communities/search", useLogin(showCommunitySearch)).Methods("GET").Queries("query", "{search}")
	r.HandleFunc("/communities/{id:[0-9]+}", useLogin(showCommunity)).Methods("GET")
	r.HandleFunc("/communities/{id:[0-9]+}/hot", useLogin(showPopularPosts)).Methods("GET")
	r.HandleFunc("/communities/{id:[0-9]+}/posts", requireLogin(createPost)).Methods("POST")
	r.HandleFunc("/communities/{id:[0-9]+}/favorite", requireLogin(addCommunityFavorite)).Methods("POST")
	r.HandleFunc("/communities/{id:[0-9]+}/unfavorite", requireLogin(deleteCommunityFavorite)).Methods("POST")

	// Activiy Feed route.
	r.HandleFunc("/activity", requireLogin(showActivityFeed)).Methods("GET")

	// Message routes.
	r.HandleFunc("/messages", requireLogin(showMessages)).Methods("GET")
	r.HandleFunc("/messages", requireLogin(sendMessage)).Methods("POST")
	r.HandleFunc("/messages/{id:[0-9]+}/delete", requireLogin(deleteMessage)).Methods("POST")
	r.HandleFunc("/messages/{username}", requireLogin(showConversation)).Methods("GET")
	r.HandleFunc("/conversations/{id:[0-9]+}", requireLogin(showGroupChat)).Methods("GET")
	r.HandleFunc("/conversations/create", requireLogin(showCreateGroupChat)).Methods("GET")
	r.HandleFunc("/conversations/create", requireLogin(createGroupChat)).Methods("POST")
	r.HandleFunc("/conversations/{id:[0-9]+}/edit", requireLogin(showEditGroupChat)).Methods("GET")
	r.HandleFunc("/conversations/{id:[0-9]+}/edit", requireLogin(editGroupChat)).Methods("POST")
	r.HandleFunc("/conversations/{id:[0-9]+}/leave", requireLogin(leaveGroupChat)).Methods("POST")
	r.HandleFunc("/conversations/{id:[0-9]+}/delete", requireLogin(deleteGroupChat)).Methods("POST")

	// Notification routes.
	r.HandleFunc("/check_update.json", requireLogin(getNotificationCounts)).Methods("GET")
	r.HandleFunc("/notifications", requireLogin(showNotifications)).Methods("GET")
	r.HandleFunc("/notifications/friend_requests", requireLogin(showFriendRequests)).Methods("GET")

	// Settings routes.
	r.HandleFunc("/settings/profile", requireLogin(showProfileSettings)).Methods("GET")
	r.HandleFunc("/settings/profile", requireLogin(editProfileSettings)).Methods("POST")
	r.HandleFunc("/region", requireLogin(getRegion)).Methods("POST")
	r.HandleFunc("/miis", getMii).Methods("POST")
	r.HandleFunc("/migrate/{id:[0-9]+}", requireLogin(migratePosts)).Methods("POST")
	r.HandleFunc("/rollback/{id:[0-9]+}", requireLogin(rollbackImport)).Methods("POST")
	r.HandleFunc("/settings/account", requireLogin(showAccountSettings)).Methods("GET")
	r.HandleFunc("/settings/account", requireLogin(editAccountSettings)).Methods("POST")
	r.HandleFunc("/blocked", requireLogin(showBlocked)).Methods("GET")

	// Help page routes.
	r.HandleFunc("/help/rules", useLogin(showRulesPage)).Methods("GET")
	r.HandleFunc("/help/faq", useLogin(showFAQPage)).Methods("GET")
	r.HandleFunc("/help/legal", useLogin(showLegalPage)).Methods("GET")
	r.HandleFunc("/help/contact", useLogin(showContactPage)).Methods("GET")

	// Image upload route.
	r.HandleFunc("/upload", uploadImage).Methods("POST")

	// Admin routes.
	r.HandleFunc("/admin", requireLogin(showAdminDashboard)).Methods("GET")
	r.HandleFunc("/reports/{id:[0-9]+}/ignore", requireLogin(reportIgnore)).Methods("POST")
	r.HandleFunc("/admin/manage", requireLogin(showAdminManagerList)).Methods("GET")
	r.HandleFunc("/admin/manage/bantemp", requireLogin(adminBanUser)).Methods("POST")
	r.HandleFunc("/admin/manage/unbantemp", requireLogin(adminUnbanUser)).Methods("POST")
	//r.HandleFunc("/admin/manage/{table}", requireLogin(showAdminManager)).Methods("GET")
	//r.HandleFunc("/admin/manage/{table}/{id:[0-9]+}", requireLogin(showAdminEditor)).Methods("GET", "POST")
	r.HandleFunc("/admin/settings", requireLogin(showAdminSettings)).Methods("GET", "POST")
	r.HandleFunc("/admin/audit_log", requireLogin(showAdminAuditLog)).Methods("GET")

	// Websocket route.
	r.HandleFunc("/ws", requireLogin(handleConnections)).Methods("GET")

	// Add a 404 page.
	r.NotFoundHandler = useLogin(handle404)

	// Serve static assets.
	r.PathPrefix("/assets/").Handler(http.StripPrefix("/assets/", http.FileServer(http.Dir("assets"))))
	// serve images as /images even though this can be changed
	r.PathPrefix("/images/").Handler(http.StripPrefix("/images/", http.FileServer(http.Dir("images"))))

	if !settings.CSRFProtectDisable {
		r.Use(CSRF)
	}
	if settings.GzipEnabled {
		r.Use(gziphandler.GzipHandler)
	}

	// Tell the http server to handle routing with the router we just made.
	http.Handle("/", r)
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
