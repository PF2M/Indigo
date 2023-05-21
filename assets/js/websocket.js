var waitingTime = 1000;
var conn;

function initWebsockets() {
    conn = new WebSocket('ws' + (window.location.protocol === 'https' ? 's' : '') + '://' + window.location.host + '/ws');
    conn.onopen = function() {
	    console.log("Connection established!");
	    var message = '{"type":"onPage","content":"'+ window.location.pathname +'"}';
	    conn.send(message);
    };

    conn.onmessage = function(e) {
        var message = JSON.parse(e.data);
        switch(message.type) {
            case "comment":
                $('.reply-list').append(message.content);
                $('.post').last().hide().fadeIn(400);
                Olv.Entry.incrementReplyCount(1);
                break;
            case "commentPreview":
                $('#' + message.id).find('.recent-reply-content').remove();
                $('#' + message.id).find('.post-meta').after(message.content);
                var commentCount = parseInt($('#' + message.id).find('.reply-count').text());
                $('#' + message.id).find('.reply-count').text(commentCount + 1);
                if(commentCount > 1) {
                    $('#' + message.id).find('.recent-reply-content').prepend('<div class="recent-reply-read-more-container" tabindex="0">View all comments (' + (commentCount + 1) + ')</div>');
                }
                break;
            case "notif":
                $('#global-menu-news').find('.badge').text(message.content);
                if(message.content > 0) {
                    $('#global-menu-news .badge').show();
                } else {
                    $('#global-menu-news .badge').hide();
                }
                getNewFaviconBadge();
                break;
            case "messageNotif":
                $('#global-menu-message').find('.badge').text(message.content);
                if(message.content > 0) {
                    $('#global-menu-message .badge').show();
                } else {
                    $('#global-menu-message .badge').hide();
                }
                getNewFaviconBadge();
                break;
            case "postYeah":
                if(window.location.pathname.substr(1, 5) == "posts") {
                    var yeahCount = parseInt($('#the-post').find('.yeah-count').text());
                    $('#the-post').find('.yeah-count').text(yeahCount + 1);
                    $('#yeah-content').removeClass('none').prepend(message.content);
                } else {
                    var yeahCount = parseInt($('#' + message.id).find('.yeah-count').text());
                    $('#' + message.id).find('.yeah-count').text(yeahCount + 1);
                }
                break;
            case "postUnyeah":
                if(window.location.pathname.substr(1, 5) == "posts") {
                    var yeahCount = parseInt($('#the-post').find('.yeah-count').text());
                    $('#the-post').find('.yeah-count').text(yeahCount - 1);
                    $('#yeah-content').find('#' + message.content).remove();
                    if(yeahCount - 1 == 0) {
                        $('#yeah-content').addClass('none');
                    }
                } else {
                    var yeahCount = parseInt($('#' + message.id).find('.yeah-count').text());
                    $('#' + message.id).find('.yeah-count').text(yeahCount - 1);
                }
                break;
            case "postEdit":
                if($("#post-content").length) {
                    $('#post-content').find('.post-content-text').html(message.content);
                } else {
                    $('#' + message.id).find('.post-content-text').html(message.content);
                }
                break;
            case "pollVote":
                var pollOption = $(".poll-option[option-id=" + message.id + "]");
                pollOption.attr("votes", parseInt(pollOption.attr("votes")) + 1);
                recalculateVotes(pollOption.siblings(".poll-option").addBack());
                break;
            case "pollUnvote":
                var pollOption = $(".poll-option[option-id=" + message.id + "]");
                pollOption.attr("votes", pollOption.attr("votes") - 1);
                recalculateVotes(pollOption.siblings(".poll-option").addBack());
                break;
            case "pollChange":
                var pollOption = $(".poll-option[option-id=" + message.id + "]");
                pollOption.attr("votes", parseInt(pollOption.attr("votes")) + 1);
                var oldPollOption = $(".poll-option[option-id=" + message.content + "]");
                oldPollOption.attr("votes", oldPollOption.attr("votes") - 1);
                recalculateVotes(pollOption.siblings(".poll-option").addBack());
                break;
            case "commentYeah":
                if(window.location.pathname.substr(1, 8) == "comments") {
                    var yeahCount = parseInt($('#the-post').find('.yeah-count').text());
                    $('#the-post').find('.yeah-count').text(yeahCount + 1);
                    $('#yeah-content').removeClass('none').prepend(message.content);
                } else {
                    var yeahCount = parseInt($('#' + message.id).find('.yeah-count').text());
                    $('#' + message.id).find('.yeah-count').text(yeahCount + 1);
                }
                break;
            case "commentUnyeah":
                if(window.location.pathname.substr(1, 8) == "comments") {
                    var yeahCount = parseInt($('#the-post').find('.yeah-count').text());
                    $('#the-post').find('.yeah-count').text(yeahCount - 1);
                    $('#yeah-content').find('#' + message.content).remove();
                    if(yeahCount - 1 == 0) {
                        $('#yeah-content').addClass('none');
                    }
                } else {
                    var yeahCount = parseInt($('#' + message.id).find('.yeah-count').text());
                    $('#' + message.id).find('.yeah-count').text(yeahCount - 1);
                }
                break;
            case "commentEdit":
                $('#' + message.id).find('.reply-content-text').html(message.content);
                break;
            case "follow":
                var followCount = parseInt($('.test-follower-count').text());
                $('.test-follower-count').text(followCount + 1);
                break;
            case "unfollow":
                var followCount = parseInt($('.test-follower-count').text());
                $('.test-follower-count').text(followCount - 1);
                break;
            case "online":
                $('.icon-container[username="' + message.content + '"].offline').removeClass('offline').addClass('online');
                break;
            case "offline":
                $('.icon-container[username="' + message.content + '"].online').removeClass('online').addClass('offline');
                break;
            case "block":
                if(window.location.pathname.startsWith("/users/" + message.content)) {
                    Olv.Form.toggleDisabled($('.post:has(.icon-container[username="' + message.content + '"])').find(".yeah-button"), true);
                } else {
                    $('.post:not(#post-content):has(.icon-container[username="' + message.content + '"])').remove();
                }
                Olv.Form.toggleDisabled($('#post-content:has(.icon-container[username="' + message.content + '"])').find(".yeah-button"), true);
                break;
            case "unblock":
                Olv.Form.toggleDisabled($('.post:has(.icon-container[username="' + message.content + '"])').find(".yeah-button"), false);
                break;
            case "delete":
                $('.post#' + message.id).remove();
                break;
            case "refresh":
                location.reload();
                break;
            case "post":
                $('.post-list').prepend(message.content);
                $('.post').first().hide().fadeIn(400);
                $(".no-post-content").addClass("none");
                if(window.location.pathname.startsWith("/communities/") && !$(message.content).hasClass("pinned")) {
                    $(".pinned:not(.repost)").prependTo('.post-list');
                }
                break;
            case "message":
                $('.messages').prepend(message.content);
                $('.post').first().hide().fadeIn(400);
                $(".no-content").remove();
                break;
            case "messagePreview":
                message.content = JSON.parse(message.content);
                if(message.content.URLType == 1) {
                    $('.list-content-with-icon-and-text li[data-href="/conversations/' + message.content.ByUsername + '"]').remove();
                } else {
                    $('.list-content-with-icon-and-text li:has(.icon-container[username="' + message.content.ByUsername + '"])').remove();
                }
                $(".list-content-with-icon-and-text").prepend(`<li class="trigger notify" data-href="/${message.content.URLType ? "conversations" : "messages"}/${message.content.ByUsername}"><a href="/${message.content.URLType ? "conversations" : "users"}/${message.content.ByUsername}" username="${message.content.URLType ? "" : message.content.ByUsername}" class="icon-container ${!message.content.ByHideOnline ? message.content.ByOnline ? 'online' : 'offline' : ''} ${message.content.ByRoleImage ? 'official-user"><img src="' + message.content.ByRoleImage + '" class=official-tag>' : '">'}<img src="${message.content.ByAvatar}" class=icon></a><div class=body><p class=title><span class=nick-name><a href="/${message.content.URLType ? "conversations" : "users"}/${message.content.ByUsername}">${message.content.URL}</a></span> <span class=id-name>${message.content.URLType ? "" : message.content.ByUsername}</span></p><span class=timestamp>${message.content.Date}</span><p class="text other${message.content.BodyText ? '">' + message.content.BodyText : "text-memo\">(attachment)"}</p></div></li>`);
                $("#global-menu-message .badge").text(parseInt($("#global-menu-message .badge").text()) + 1).show();
                getNewFaviconBadge();
                break;
            default:
                console.log(message);
                break;
        }
    }


    conn.onclose = function() {
	    if(websocketsEnabled) {
		    $('.icon-container[username="' + $("body").attr("sess-usern") + '"].online').removeClass('online').addClass('offline');
		    console.log("Disconnected, reconnecting in " + waitingTime / 1000 + " seconds");
		    setTimeout(function() {
			    waitingTime *= 2;
			    console.log("Reconnecting...");
			    initWebsockets();
		    }, waitingTime);
	    }
    }

    $(document).off("pjax:end", onPJAXEnd).on("pjax:end", onPJAXEnd);
}

function onPJAXEnd() {
	var message = '{"type":"onPage","content":"'+ window.location.pathname +'"}';
	conn.send(message);
}

function toggleWebsockets() {
	if(websocketsEnabled) {
		conn.close(1000);
		websocketsEnabled = false;
		$('.icon-container[username="' + $("body").attr("sess-usern") + '"].online').removeClass('online').addClass('offline');
		$(document).off("pjax:end", onPJAXEnd);
	} else {
		websocketsEnabled = true;
		initWebsockets();
	}
}
var websocketsEnabled = true;
initWebsockets();
