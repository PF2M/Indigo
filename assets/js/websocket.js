var httpsex = "";
if(location.protocol == "https:") {
	httpsex = "s";
}

var conn = new WebSocket('ws' + httpsex + '://' + window.location.hostname + ':' + window.location.port + '/ws');
conn.onopen = function(e) {
	console.log("Connection established!");
	message = '{"type":"onPage","content":"'+ window.location.pathname +'"}'
	conn.send(message);
};

conn.onmessage = function(e) {
	message = JSON.parse(e.data);
	if (message.type == "comment") {
		$('.reply-list').append(message.content);
		$('.post').last().hide().fadeIn(400);
		Olv.Entry.incrementReplyCount(1);
	} else if (message.type == "commentPreview") {
		$('#'+ message.id).find('.recent-reply-content').remove();
		$('#'+ message.id).find('.post-meta').after(message.content);
		commentCount = parseInt($('#'+ message.id).find('.reply-count').text());
		$('#'+ message.id).find('.reply-count').text(commentCount+1);
		if (commentCount > 1) {
			$('#'+ message.id).find('.recent-reply-content').prepend('<div class="recent-reply-read-more-container" tabindex="0">View all comments ('+ (commentCount+1) +')</div>')
		}
	} else if (message.type == "notif") {
		$('#global-menu-news').find('.badge').text(message.content);
		if (message.content > 0) {
			$('#global-menu-news .badge').show();
		} else {
			$('#global-menu-news .badge').hide();
		}
		getNewFaviconBadge();
	} else if (message.type == "messageNotif") {
		$('#global-menu-message').find('.badge').text(message.content);
		if (message.content > 0) {
			$('#global-menu-message .badge').show();
		} else {
			$('#global-menu-message .badge').hide();
		}
		getNewFaviconBadge();
	} else if (message.type == "postYeah") {
		if (window.location.pathname.substr(1,5) == "posts") {
			yeahCount = parseInt($('#the-post').find('.yeah-count').text());
			$('#the-post').find('.yeah-count').text(yeahCount+1);
			$('#yeah-content').removeClass('none').prepend(message.content)
		} else {
			yeahCount = parseInt($('#'+ message.id).find('.yeah-count').text());
			$('#'+ message.id).find('.yeah-count').text(yeahCount+1);
		}
	} else if (message.type == "postUnyeah") {
		if (window.location.pathname.substr(1,5) == "posts") {
			yeahCount = parseInt($('#the-post').find('.yeah-count').text());
			$('#the-post').find('.yeah-count').text(yeahCount-1);
			$('#yeah-content').find('#'+ message.content).remove()
			if (yeahCount-1 == 0) {
				$('#yeah-content').addClass('none')
			}
		} else {
			yeahCount = parseInt($('#'+ message.id).find('.yeah-count').text());
			$('#' + message.id).find('.yeah-count').text(yeahCount-1);
		}
	} else if (message.type == "postEdit") {
		if($("#post-content").length) {
			$('#post-content').find('.post-content-text').html(message.content);
		} else {
			$('#' + message.id).find('.post-content-text').html(message.content);
		}
	} else if (message.type == "pollVote") {
		var pollOption = $(".poll-option[option-id=" + message.id + "]");
		pollOption.attr("votes", parseInt(pollOption.attr("votes")) + 1);
		recalculateVotes(pollOption.siblings(".poll-option").addBack());
	} else if (message.type == "pollUnvote") {
		var pollOption = $(".poll-option[option-id=" + message.id + "]");
		pollOption.attr("votes", pollOption.attr("votes") - 1);
		recalculateVotes(pollOption.siblings(".poll-option").addBack());
	} else if (message.type == "pollChange") {
		var pollOption = $(".poll-option[option-id=" + message.id + "]");
		pollOption.attr("votes", parseInt(pollOption.attr("votes")) + 1);
		var oldPollOption = $(".poll-option[option-id=" + message.content + "]");
		oldPollOption.attr("votes", oldPollOption.attr("votes") - 1);
		recalculateVotes(pollOption.siblings(".poll-option").addBack());
	} else if (message.type == "commentYeah") {
		if (window.location.pathname.substr(1,8) == "comments") {
			yeahCount = parseInt($('#the-post').find('.yeah-count').text());
			$('#the-post').find('.yeah-count').text(yeahCount+1);
			$('#yeah-content').removeClass('none').prepend(message.content)
		} else {
			yeahCount = parseInt($('#'+ message.id).find('.yeah-count').text());
			$('#'+ message.id).find('.yeah-count').text(yeahCount+1);
		}
	} else if (message.type == "commentUnyeah") {
		if (window.location.pathname.substr(1,8) == "comments") {
			yeahCount = parseInt($('#the-post').find('.yeah-count').text());
			$('#the-post').find('.yeah-count').text(yeahCount-1);
			$('#yeah-content').find('#'+ message.content).remove()
			if (yeahCount-1 == 0) {
				$('#yeah-content').addClass('none')
			}
		} else {
			yeahCount = parseInt($('#'+ message.id).find('.yeah-count').text());
			$('#'+ message.id).find('.yeah-count').text(yeahCount-1);
		}
	} else if (message.type == "commentEdit") {
		$('#'+ message.id).find('.reply-content-text').html(message.content);
	} else if (message.type == "follow") {
		followCount = parseInt($('.test-follower-count').text());
		$('.test-follower-count').text(followCount + 1);
	} else if (message.type == "unfollow") {
		followCount = parseInt($('.test-follower-count').text());
		$('.test-follower-count').text(followCount - 1);
	} else if (message.type == "online") {
		$('.icon-container[username="' + message.content + '"].offline').removeClass('offline').addClass('online');
	} else if (message.type == "offline") {
		$('.icon-container[username="' + message.content + '"].online').removeClass('online').addClass('offline');
	} else if (message.type == "block") {
		if(window.location.pathname.startsWith("/users/" + message.content)) {
			Olv.Form.toggleDisabled($('.post:has(.icon-container[username="' + message.content + '"])').find(".yeah-button"), true);
		} else {
			$('.post:not(#post-content):has(.icon-container[username="' + message.content + '"])').remove();
		}
		Olv.Form.toggleDisabled($('#post-content:has(.icon-container[username="' + message.content + '"])').find(".yeah-button"), true);
	} else if (message.type == "unblock") {
		Olv.Form.toggleDisabled($('.post:has(.icon-container[username="' + message.content + '"])').find(".yeah-button"), false);
	} else if (message.type == "delete") {
		$('.post#' + message.id).remove();
	} else if (message.type == "refresh") {
		location.reload();
	} else if (message.type == "post") {
		$('.post-list').prepend(message.content);
		$('.post').first().hide().fadeIn(400);
		$(".no-post-content").addClass("none");
		if(window.location.pathname.startsWith("/communities/") && !$(message.content).hasClass("pinned")) {
			$(".pinned:not(.repost)").prependTo('.post-list');
		}
	} else if (message.type == "message") {
		$('.messages').prepend(message.content);
		$('.post').first().hide().fadeIn(400);
		$(".no-content").remove();
	} else if (message.type == "messagePreview") {
		message.content = JSON.parse(message.content);
		if(message.content.URLType == 1) {
			$('.list-content-with-icon-and-text li[data-href="/conversations/' + message.content.ByUsername + '"]').remove();
		} else {
			$('.list-content-with-icon-and-text li:has(.icon-container[username="' + message.content.ByUsername + '"])').remove();
		}
		$(".list-content-with-icon-and-text").prepend(`<li class="trigger notify" data-href="/${message.content.URLType ? "conversations" : "messages"}/${message.content.ByUsername}"><a href="/${message.content.URLType ? "conversations" : "users"}/${message.content.ByUsername}" username="${message.content.URLType ? "" : message.content.ByUsername}" class="icon-container ${!message.content.ByHideOnline ? message.content.ByOnline ? 'online' : 'offline' : ''} ${message.content.ByRoleImage ? 'official-user"><img src="' + message.content.ByRoleImage + '" class=official-tag>' : '">'}<img src="${message.content.ByAvatar}" class=icon></a><div class=body><p class=title><span class=nick-name><a href="/${message.content.URLType ? "conversations" : "users"}/${message.content.ByUsername}">${message.content.URL}</a></span> <span class=id-name>${message.content.URLType ? "" : message.content.ByUsername}</span></p><span class=timestamp>${message.content.Date}</span><p class="text other${message.content.BodyText ? '">' + message.content.BodyText : "text-memo\">(attachment)"}</p></div></li>`);
		$("#global-menu-message .badge").text(parseInt($("#global-menu-message .badge").text()) + 1).show();
		getNewFaviconBadge();
	} else {
		console.log(message);
	}
};

conn.onclose = function() {
	if(websocketsEnabled == true) {
		$('.icon-container[username="' + $("body").attr("sess-usern") + '"].online').removeClass('online').addClass('offline');
		$("#wrapper").after('<div id="cookie-policy-notice"><div class="cookie-content"><p>You\'ve been disconnected from the server. Attempt to reconnect?</p><button class="cookie-policy-notice" onclick="$(\'#websockets\').remove();$(\'<script>\').attr(\'src\', \'/assets/js/websocket.js\').attr(\'id\', \'websockets\').appendTo(\'head\');$(\'#cookie-policy-notice\').remove()">Yes</button><a class="cookie-setting" onclick="$(\'#cookie-policy-notice\').remove()">No</a></div></div>');
	}
};

function onPJAXEnd() {
	message = '{"type":"onPage","content":"'+ window.location.pathname +'"}'
	conn.send(message);
}
$(document).on("pjax:end", onPJAXEnd);

var websocketsEnabled = true;
function toggleWebsockets() {
	if(websocketsEnabled == true) {
		conn.close(1000);
		websocketsEnabled = false;
		$('.icon-container[username="' + $("body").attr("sess-usern") + '"].online').removeClass('online').addClass('offline');
		$(document).off("pjax:end", onPJAXEnd);
	} else {
		$("#websockets").remove();
		$('<script>').attr('src', "/assets/js/websocket.js").attr("id", "websockets").appendTo('head');
	}
}