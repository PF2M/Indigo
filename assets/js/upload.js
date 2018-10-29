function checkForm() {
    if($("textarea[name=body]").val().length > 0 || $("input[name=image]").val().length > 0) {
        Olv.Form.toggleDisabled($("input.post-button"), false);
        Olv.Form.toggleDisabled($("input.reply-button"), false);
    } else {
        Olv.Form.toggleDisabled($("input.post-button"), true);
        Olv.Form.toggleDisabled($("input.reply-button"), true);
    }
}
function showError(error) {
    console.log(error);
    $("input[name=image]").val("");
    if(!$(".file-upload-button").hasClass("for-avatar")) {
        $(".preview-container").css("display", "none");
        checkForm();
    }
    $(".file-button").removeAttr("disabled");
    $(".file-button").val(null);
    $(".file-upload-button").text("Upload");
    Olv.showMessage("Attachment upload failed", "There was an error trying to upload your attachment.\nThe response received from the server was this:\n" + error.responseText);
}
function postFile(base64, fileType, isDrawing) {
    $.post("/upload", Olv.Form.csrftoken({file: base64}), function(data) {
        if(isDrawing) {
            $("input[name=painting]").val(data);
        } else {
            $("input[name=image]").val(data);
        }
        if(fileType.startsWith("audio/")) {
            $(".preview-audio").attr("src", base64);
            $(".preview-image").addClass("none");
            $(".preview-audio").removeClass("none");
            $(".preview-video").addClass("none");
            $("input[name=attachment_type]").val("1");
        } else if(fileType.startsWith("video/")) {
            $(".preview-video").attr("src", base64);
            $(".preview-image").addClass("none");
            $(".preview-audio").addClass("none");
            $(".preview-video").removeClass("none");
            $("input[name=attachment_type]").val("2");
        } else {
            $(".preview-image").attr("src", base64);
            $(".preview-image").removeClass("none");
            $(".preview-audio").addClass("none");
            $(".preview-video").addClass("none");
            $("input[name=attachment_type]").val("0");
        }
        if(!isDrawing && !$(".file-upload-button").hasClass("for-avatar")) {
            $(".preview-container").attr("style", "");
            checkForm();
        } else if(!isDrawing) {
            $("input[name=avatar][value=0]").prop("checked", true).change();
        } else {
            $("#drawing").remove();
            $(".textarea-memo").append("<img id=\"drawing\" src=\"" + base64 + "\" style=\"background:white;\"></img>");
        }
        if(isDrawing) {
            Olv.Form.toggleDisabled($("input.post-button"), false);
            Olv.Form.toggleDisabled($(".memo-finish-btn"), false);
        } else {
            Olv.Form.toggleDisabled($(".file-button"), false);
            $(".file-button").val(null);
            $(".file-upload-button").text("Upload");
        }
    }, "text").fail(function(error) {
        showError(error);
    });
}
function init() {
    $(".file-button").off().on("change", function(event) {
        console.log(event);
        if(this.files.length) {
            Olv.Form.toggleDisabled($("input.post-button"), true);
            $(".file-button").attr("disabled", "disabled");
            $(".file-upload-button").text("Uploading...");
            var fileType = this.files[0].type;
            var reader = new FileReader();
            reader.readAsDataURL(this.files[0]);
            reader.onload = function () {
                var base64;
                if($(".file-upload-button").hasClass("for-avatar") && fileType != "image/gif") {
                    var img = new Image();
                    img.src = reader.result;
                    img.onload = function() {
                        var canvas = document.createElement("canvas");
                        var ctx = canvas.getContext("2d");
                        var size = 96, factor, startX, startY, resizeWidth, resizeHeight;
                        canvas.width = size;
                        canvas.height = size;
                        if(img.width > img.height) {
                            factor = img.width / img.height;
                            startX = (img.width - img.height) / 2;
                            startY = 0;
                            resizeWidth = size * factor;
                            resizeHeight = size;
                        } else if(img.height > img.width) {
                            factor = img.height / img.width;
                            startX = 0;
                            startY = (img.height - img.width) / 2;
                            resizeWidth = size;
                            resizeHeight = size * factor;
                        } else {
                            factor = 1;
                            startX = 0;
                            startY = 0;
                            resizeWidth = size;
                            resizeHeight = size;
                        }
                        ctx.drawImage(img, startX, startY, img.width, img.height, 0, 0, resizeWidth, resizeHeight);
                        postFile(canvas.toDataURL(), fileType, false);
                    }
                } else {
                    postFile(reader.result, fileType, false);
                }

            };
            reader.onerror = function (error) {
                showError(error);
            };
        } else {
            $("input[name=image]").val("");
            if(!$(".file-upload-button").hasClass("for-avatar")) {
                $(".preview-container").css("display", "none");
                checkForm();
            }
        }
    });
    $(document).on("dragover dragenter", function(event) {
        event.stopPropagation();
        event.preventDefault();
        event.originalEvent.dataTransfer.dropEffect = "copy";
    });
    $(document).on("drop paste", function(event) {
        if($(".file-button").attr("disabled")) {
            return;
        }
        var files;
        switch(event.type) {
            case "drop":
                files = event.originalEvent.dataTransfer.files;
                break;
            case "paste":
                if(event.originalEvent.clipboardData.files.length == 0) {
                    return;
                }
                files = event.originalEvent.clipboardData.files;
                break;
            default:
                return;
        }
        event.stopPropagation();
        event.preventDefault();

        if(files[0].type.startsWith("image/") || files[0].type.startsWith("audio/") || files[0].type.startsWith("video/")) {
            $(".file-button")[0].files = files;
        } else {
            Olv.showMessage("Attachment upload failed", "You can only upload images, audio or videos.");
        }
    });
}

$(document).off("ready, pjax:end", init).on("ready, pjax:end", init);
init();