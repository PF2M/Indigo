// All the types/structures defined in Indigo.

package main

import (
	"database/sql"
	"html/template"
	"sync"
	"time"
)

// Variable declarations for admin settings.
type adminConfig struct {
	Manage struct {
		MinimumLevel int
	}
	Settings struct {
		MinimumLevel int
	}
}

// Variable declarations for comments.
type comment struct {
	ID                        int
	CreatedBy                 int
	PostID                    int
	CreatedAt                 string
	CreatedAtUnix             int64
	EditedAt                  string
	EditedAtUnix              int64
	Feeling                   int
	BodyText                  string
	Body                      template.HTML
	Image                     string
	AttachmentType            int
	URL                       string
	URLType                   int
	Pinned                    bool
	IsSpoiler                 bool
	IsRMByAdmin               bool
	PostType                  int
	CommenterUsername         string
	CommenterNickname         string
	CommenterIcon             string
	CommenterHasMii           bool
	CommenterOnline           bool
	CommenterHideOnline       bool
	CommenterColor            string
	CommenterRoleImage        string
	CommenterRoleOrganization string
	Yeahed                    bool
	YeahCount                 int
	ByMe                      bool
	ByMii                     bool
	CanYeah                   bool
}

// Variable declarations for communities.
type community struct {
	ID              int
	Title           string
	DescriptionText string
	Description     template.HTML
	Icon            string
	Banner          string
	IsFeatured      bool
	Permissions     int
	RM              bool
}

// Variable declarations for settings.
type config struct {
	Port string
	SSL  struct {
		Enabled     bool
		Certificate string
		Key         string
	}
	DB struct {
		Host     string
		Username string
		Password string
		Name     string
	}
	ImageHost struct {
		Provider      string
		Username      string
		APIEndpoint   string
		APIPublic     string
		APISecret     string
		ImageEndpoint string
		UploadPreset  string
		MaxUploadSize string
	}
	Webhooks struct {
		Enabled bool
		Reports string
		Signups string
		Logins  string
	}
	ReCAPTCHA struct {
		Enabled   bool
		SiteKey   string
		SecretKey string
	}
	SMTP struct {
		Enabled  bool
		Hostname string
		Port     string
		Email    string
		Password string
	}
	CSRFSecret      string
	IPHubKey        string
	Proxy           bool
	ForceLogins     bool
	AllowSignups    bool
	DefaultTimezone string
	ReportReasons   []reportReason
	TextToReplace   []struct {
		Original string
		Replaced string
	}
	EmoteLimit int
}

// Variable declarations for conversations.
type conversation struct {
	ID         int
	Target     int
	Nickname   string
	Username   string
	Online     bool
	HideOnline bool
	Color      string
	Icon       string
	HasMii     bool
	RoleImage  string
	CreatedBy  int
	BodyText   string
	Body       template.HTML
	Image      string
	PostType   int
	Date       string
	DateUnix   int64
	Read       bool
}

// Variable declarations for friend requests.
type friendRequest struct {
	ID                 int
	By                 int
	CreatedAt          string
	CreatedAtUnix      int64
	Date               string
	Message            string
	Read               bool
	ByUsername         string
	ByAvatar           string
	ByHasMii           bool
	ByNickname         string
	ByOnline           bool
	ByHideOnline       bool
	ByColor            string
	ByRoleImage        string
	ByRoleOrganization string
}

// Variable declarations for import log entries.
type importLog struct {
	ID       int
	Image    string
	Username string
}

// Variable declarations for messages.
type message struct {
	ID             int
	Date           string
	DateUnix       int64
	Feeling        int
	BodyText       string
	Body           template.HTML
	Image          string
	AttachmentType int
	URL            string
	URLType        int
	PostType       int
	ByUsername     string
	ByAvatar       string
	ByHasMii       bool
	ByOnline       bool
	ByHideOnline   bool
	ByColor        string
	ByRoleImage    string
	ByMe           bool
}

// Variable declarations for migrations.
type migration struct {
	Success int    `json:"success"`
	Error   string `json:"error"`
	Posts   []struct {
		ID             interface{} `json:"id"`
		CreatedBy      interface{} `json:"created_by"`
		CommunityID    interface{} `json:"community_id"`
		CreatedAt      string      `json:"created_at"`
		EditedAt       string      `json:"edited_at"`
		Feeling        int         `json:"feeling"`
		Body           string      `json:"body"`
		Image          string      `json:"image"`
		AttachmentType int         `json:"attachment_type"`
		URL            string      `json:"url"`
		IsSpoiler      interface{} `json:"is_spoiler"`
		IsRM           int         `json:"is_rm"`
		IsRMByAdmin    int         `json:"is_rm_by_admin"`
		PostType       int         `json:"post_type"`
	} `json:"posts"`
	Communities []struct {
		ID    interface{} `json:"id"`
		Title string      `json:"name"`
		Icon  string      `json:"icon"`
	} `json:"communities"`
}

// Variable declarations for the migration options.
type migrationOption struct {
	ID               int
	Image            string
	PasswordRequired bool
}

// Variable declarations for notifications.
type notification struct {
	ID             int
	Type           int
	By             int
	Post           sql.NullInt64
	Date           string
	DateUnix       int64
	Read           bool
	MergedCount    int
	MergedOthers   int
	URL            string
	ByUsername     string
	ByAvatar       string
	ByHasMii       bool
	ByNickname     string
	ByOnline       bool
	ByHideOnline   bool
	ByColor        string
	ByRoleImage    string
	PostText       string
	PostType       int
	PostIsRM       bool
	MergedUsername [3]string
	MergedNickname [3]string
	MergedColor    [3]string
}

// Variable declarations for notification counts.
type notificationCount struct {
	Messages      int
	Notifications int
}

// Variable declarations for poll options.
type option struct {
	ID         int
	Name       string
	Votes      float64
	Percentage float64
	Selected   bool
}

// Variable declarations for polls.
type poll struct {
	ID       int
	Votes    float64
	Options  []option
	Selected bool
}

// Variable declarations for posts.
type post struct {
	ID                     int
	Type                   int
	CreatedBy              int
	CreatedAt              string
	CreatedAtTime          time.Time
	CreatedAtUnix          int64
	EditedAt               string
	EditedAtTime           time.Time
	EditedAtUnix           int64
	Feeling                int
	Body                   template.HTML
	BodyText               string
	Image                  string
	AttachmentType         int
	URL                    string
	URLType                int
	Pinned                 bool
	Privacy                int
	IsSpoiler              bool
	IsRM                   bool
	IsRMByAdmin            bool
	ByMe                   bool
	Poll                   poll
	HasPoll                bool
	PostType               int
	Repost                 *post
	RepostID               int
	MigratedID             string
	MigratedCommunity      string
	MigrationID            int
	MigrationImage         string
	MigrationURL           string
	PosterUsername         string
	PosterNickname         string
	PosterIcon             string
	PosterHasMii           bool
	PosterOnline           bool
	PosterHideOnline       bool
	PosterColor            string
	PosterRoleID           int
	PosterRoleImage        string
	PosterRoleOrganization string
	CommunityID            int
	CommunityName          string
	CommunityIcon          string
	CommunityRM            bool
	Yeahed                 bool
	CanYeah                bool
	YeahCount              int
	CommentCount           int
	CommentPreview         comment
}

// Variable declarations for profiles.
type profile struct {
	User              int
	CreatedAt         string
	CreatedAtUnix     int64
	NNID              string
	MiiHash           string
	AvatarImage       string
	AvatarID          int
	Gender            string
	Region            string
	Discord           string
	Twitter           string
	SwitchCode        string
	PSN               string
	YouTube           string
	Steam             string
	AllowFriend       int
	CommentText       string
	Comment           template.HTML
	NNIDVisibility    int
	YeahVisibility    int
	ReplyVisibility   int
	FavoritePostID    int
	FavoritePostImage string
	FriendCount       int
	FollowingCount    int
	FollowerCount     int
	PostCount         int
	CommentCount      int
	YeahCount         int
}

// Variable declarations for profile sidebars.
type profileSidebar struct {
	User                user
	CurrentUser         user
	Profile             profile
	ProfileOnPage       string
	IsFollowing         bool
	IsFollowingMe       bool
	Reasons             []reportReason
	FavoriteCommunities []community
	FriendStatus        int
	Request             friendRequest
	RequestTime         string
}

// Variable declarations for reports.
type report struct {
	ID         int
	Type       int
	Message    string
	Reason     int
	ByID       int
	ByUsername string
	ByNickname string
	ByColor    string
	Post       *post
	User       *user
}

// Variable declarations for report reasons.
type reportReason struct {
	Name         string
	Message      string
	Enabled      bool
	BodyRequired bool
}

// Variable declarations for repost previews.
type repostPreview struct {
	ID       int
	Nickname string
	Text     string
	PostType int
}

// Variable declarations for users.
type user struct {
	ID       int
	Username string
	Nickname string
	Avatar   string
	HasMii   bool
	Email    string
	Password string
	IP       string
	Level    int
	Role     struct {
		Image        string
		Organization string
	}
	Online            bool
	HideOnline        bool
	Color             string
	Theme             string
	ThemeColors       []string
	LastSeen          string
	LastSeenUnix      int64
	HideLastSeen      bool
	Comment           string
	YeahNotifications bool
	LightMode         bool
	WebsocketsEnabled bool
	DefaultPrivacy    int
	Blocked           bool
	Timezone          string
	Notifications     notificationCount
	ForbiddenKeywords string
	CSRFToken         string
}

// Variable declarations for websocket messages.
type wsMessage struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Content string `json:"content"`
}

// Variable declarations for websocket sessions.
type wsSession struct {
	Connected bool
	UserID    int
	Level     int
	OnPage    string
	Mutex    *sync.Mutex
}

// Variable declarations for Yeahs.
type yeah struct {
	ID       int
	Username string
	Avatar   string
	HasMii   bool
	Role     string
}
