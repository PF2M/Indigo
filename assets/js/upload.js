function checkForm() {
    if($("textarea[name=body]").length && ($("textarea[name=body]").val().length > 0 || $("input[name=image]").val().length > 0)) {
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
function postFile(file, fileType, isDrawing, inputName) {
    if(!fileType.startsWith("image/") && !fileType.startsWith("audio/") && !fileType.startsWith("video/")) return $("input[name=image]").val(""), Olv.showMessage("Error", "Invalid file type."), $(".file-button").removeAttr("disabled"), $(".file-button").val(null), void $(".file-upload-button").text("Upload");

    var formData = new FormData();
    formData.append(inputName, file);
    var csrfTokenData = Olv.Form.csrftoken({});
    formData.append('csrfmiddlewaretoken', csrfTokenData.csrfmiddlewaretoken);

    $.ajax({
        url: '/upload',
        type: 'POST',
        data: formData,
        cache: false,
        contentType: false,
        processData: false,
        success: function(data) {
            if(isDrawing) {
                $("input[name=painting]").val(data);
            } else {
                $("input[name=" + inputName + "]").val(data);
            }
            if(fileType.startsWith("audio/")) {
                $(".preview-audio").attr("src", URL.createObjectURL(file));
                $(".preview-image").addClass("none");
                $(".preview-audio").removeClass("none");
                $(".preview-video").addClass("none");
                $("input[name=attachment_type]").val("1");
            } else if(fileType.startsWith("video/")) {
                $(".preview-video").attr("src", URL.createObjectURL(file));
                $(".preview-image").addClass("none");
                $(".preview-audio").addClass("none");
                $(".preview-video").removeClass("none");
                $("input[name=attachment_type]").val("2");
            } else if($(".file-upload-button").hasClass("for-avatar")) {
                $(".preview-image").attr("src", URL.createObjectURL(file));
                $(".preview-image").removeClass("none");
            } else {
                $("input[name=" + inputName + "]").siblings(".screenshot-container").children(".preview-image").attr("src", URL.createObjectURL(file));
                $("input[name=" + inputName + "]").siblings(".screenshot-container").children(".preview-image").removeClass("none");
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
                $(".textarea-memo").append("<img id=\"drawing\" src=\"" + URL.createObjectURL(file) + "\" style=\"background:white;\"></img>");
                Olv.Form.toggleDisabled($("input.post-button"), false);
                Olv.Form.toggleDisabled($(".memo-finish-btn"), false);
            }
            if(!isDrawing) {
                Olv.Form.toggleDisabled($(".file-button"), false);
                $(".file-button").val(null);
                $(".file-upload-button").text("Upload");
            }
        },
        error: function(error) {
            showError(error);
        }
    });
}


function handleChange(event) {
    console.log(event);
    var inputName = "image";
    if($(this).attr("id") !== undefined) inputName = $(this).attr("id");
    if(this.files.length) {
        Olv.Form.toggleDisabled($("input.post-button"), true);
        $("input[name=" + inputName + "]").siblings(".file-button").attr("disabled", "disabled");
        $("input[name=" + inputName + "]").siblings(".file-upload-button").text("Uploading...");
        var fileType = this.files[0].type;
        var file = this.files[0];
        if(($(".file-upload-button").hasClass("for-avatar") || inputName === "icon") && fileType !== "image/gif") {
            var img = new Image();
            img.src = URL.createObjectURL(file);
            img.onload = function() {
                var canvas = document.createElement("canvas");
                var ctx = canvas.getContext("2d");
                    ctx.imageSmoothingQuality = "high";
                    var size = 128, factor, startX, startY, resizeWidth, resizeHeight;
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
                    canvas.toBlob(function(blob) {
                        postFile(blob, fileType, false, inputName);
                    });
                }
            } else {
                postFile(file, fileType, false, inputName);
            }
    } else {
        $("input[name=image]").val("");
        if(!$(".file-upload-button").hasClass("for-avatar")) {
            $(".preview-container").css("display", "none");
            checkForm();
        }
    }
}

function handleDropPaste(event) {
    if($(this).siblings(".file-button").attr("disabled")) {
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
        $(".file-button").trigger("change");
    } else {
        Olv.showMessage("Attachment upload failed", "You can only upload images, audio or videos.");
    }
}

function handleDrag(event) {
    event.stopPropagation();
    event.preventDefault();
    event.originalEvent.dataTransfer.dropEffect = "copy";
}

function init() {
    $(".file-button").off().on("change", handleChange);
    $(document).off("dragover dragenter").on("dragover dragenter", handleDrag);
    $(document).off("drop paste").on("drop paste", handleDropPaste);
}

$(document).off("ready", init).off("pjax:end", init).on("ready", init).on("pjax:end", init);
init();
