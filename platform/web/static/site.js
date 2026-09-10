// Popovers, the account menu and the legend, close when you click elsewhere
// or press Escape.
document.querySelectorAll("details.menu, details.legend").forEach(function (menu) {
  document.addEventListener("click", function (event) {
    if (menu.open && !menu.contains(event.target)) {
      menu.open = false;
    }
  });
  document.addEventListener("keydown", function (event) {
    if (event.key === "Escape") {
      menu.open = false;
    }
  });
});

// A toast leaves on its own after a few seconds, or when clicked. The
// message came in the address, so the address is cleaned up too.
document.querySelectorAll(".toast").forEach(function (toast) {
  var leave = function () {
    toast.classList.add("gone");
    toast.addEventListener("animationend", function () { toast.remove(); }, { once: true });
  };
  toast.addEventListener("click", leave);
  setTimeout(leave, 4000);
});
if (window.location.search && history.replaceState) {
  var params = new URLSearchParams(window.location.search);
  params.delete("note");
  params.delete("error");
  var query = params.toString();
  history.replaceState(null, "", window.location.pathname + (query ? "?" + query : ""));
}

// Tooltips open on tap and close on the next tap anywhere, for screens
// without hover.
document.addEventListener("click", function (event) {
  var tipped = event.target.closest("[data-tip]");
  document.querySelectorAll("[data-tip].show").forEach(function (el) {
    if (el !== tipped) {
      el.classList.remove("show");
    }
  });
  if (tipped) {
    tipped.classList.toggle("show");
  }
});

// Chosen photos show as thumbnails before they upload, in the order they
// will be saved. The first is the cover, so the order can be changed by
// dragging a photo or nudging it with the arrows. A file box cannot be
// rewritten by script, so the chosen order is held here and the form is
// built from it when the post goes out.
var maxPreviews = 6;

function orderedFiles(input) {
  if (!input.ordered) {
    input.ordered = [];
  }
  return input.ordered;
}

function drawPreviews(input, strip) {
  var files = orderedFiles(input);
  strip.replaceChildren();
  files.forEach(function (file, index) {
    var item = document.createElement("div");
    item.className = "preview";
    item.draggable = true;

    var image = document.createElement("img");
    image.alt = file.name;
    image.src = URL.createObjectURL(file);
    image.onload = function () { URL.revokeObjectURL(image.src); };
    item.appendChild(image);

    var row = document.createElement("p");
    row.className = "order";
    var label = document.createElement("span");
    label.className = index === 0 ? "pos cover" : "pos";
    label.textContent = index === 0 ? "cover" : String(index + 1);
    row.appendChild(label);
    [["\u2190", -1], ["\u2192", 1]].forEach(function (pair) {
      var nudge = document.createElement("button");
      nudge.type = "button";
      nudge.className = "nudge";
      nudge.textContent = pair[0];
      nudge.title = pair[1] < 0 ? "Move earlier" : "Move later";
      nudge.disabled = (pair[1] < 0 && index === 0) || (pair[1] > 0 && index === files.length - 1);
      nudge.addEventListener("click", function () {
        moveFile(input, strip, index, index + pair[1]);
      });
      row.appendChild(nudge);
    });
    item.appendChild(row);

    item.addEventListener("dragstart", function (event) {
      item.classList.add("dragging");
      event.dataTransfer.setData("text/plain", String(index));
      event.dataTransfer.effectAllowed = "move";
    });
    item.addEventListener("dragend", function () {
      item.classList.remove("dragging");
    });
    item.addEventListener("dragover", function (event) {
      event.preventDefault();
      item.classList.add("over");
    });
    item.addEventListener("dragleave", function () {
      item.classList.remove("over");
    });
    item.addEventListener("drop", function (event) {
      event.preventDefault();
      item.classList.remove("over");
      moveFile(input, strip, parseInt(event.dataTransfer.getData("text/plain"), 10), index);
    });
    strip.appendChild(item);
  });
}

function moveFile(input, strip, from, to) {
  var files = orderedFiles(input);
  if (isNaN(from) || from === to || from < 0 || to < 0 || from >= files.length || to >= files.length) {
    return;
  }
  var moved = files.splice(from, 1)[0];
  files.splice(to, 0, moved);
  drawPreviews(input, strip);
}

document.querySelectorAll('input[type="file"][accept^="image"]').forEach(function (input) {
  var strip = document.createElement("div");
  strip.className = "previews";
  input.insertAdjacentElement("afterend", strip);
  input.addEventListener("change", function () {
    input.ordered = Array.from(input.files).slice(0, maxPreviews);
    drawPreviews(input, strip);
  });
});

// Every form says what it is doing while the server works. A photo post can
// take a while, and without this it looks like nothing happened, so people
// press the button again. Words come from the button's own label.
var busyWords = {
  post: "Posting", save: "Saving", upload: "Uploading", send: "Sending",
  create: "Creating", add: "Adding", search: "Searching", enter: "Entering",
  set: "Saving", cast: "Voting", accept: "Accepting", decline: "Declining",
  remove: "Removing", delete: "Deleting", dismiss: "Dismissing",
  build: "Saving", started: "Saving", finished: "Saving", make: "Saving",
  sign: "Signing in", clear: "Clearing", decide: "Deciding", like: "Liking",
};

function busyWordFor(button) {
  if (button.dataset.busy) {
    return button.dataset.busy;
  }
  var first = (button.textContent || "").trim().split(/\s+/)[0].toLowerCase();
  return busyWords[first] || "Working";
}

// photosAreChecked reports whether this deployment screens photos, so the
// strip can name that step instead of leaving a silent gap.
function photosAreChecked() {
  var shell = document.getElementById("busy-strip");
  return !!shell && shell.dataset.screening === "yes";
}

// afterUploadWord is what the server is doing once the last byte is up.
function afterUploadWord() {
  return photosAreChecked() ? "Checking and saving photos" : "Saving";
}

// showBusy puts the strip under the button and hands back a function that
// changes the word as the work moves on.
function showBusy(form, button, word) {
  var shell = document.getElementById("busy-strip");
  if (!shell) {
    return function () {};
  }
  var strip = shell.content.firstElementChild.cloneNode(true);
  strip.querySelector(".word").textContent = word;
  button.insertAdjacentElement("afterend", strip);
  form.setAttribute("aria-busy", "true");
  return function (next) {
    strip.querySelector(".word").textContent = next;
  };
}

function chosenFiles(form) {
  var count = 0;
  form.querySelectorAll('input[type="file"]').forEach(function (input) {
    count += input.ordered ? input.ordered.length : (input.files ? input.files.length : 0);
  });
  return count;
}

// sendWithProgress posts a form with photos through XMLHttpRequest so the
// strip can say what is really happening: reading the files, sending the
// bytes, then waiting while the server saves. A plain form post gives no
// such signal. Anything unsupported falls back to the normal submit.
// Photos leave the phone or camera far larger than the site keeps them, and
// a home connection sends perhaps a third of a megabyte a second, so four
// untouched photos can be a minute and a half of waiting. Shrinking them
// here first cuts that by more than ten times and loses nothing, because the
// server keeps them at this size anyway.
var maxPhotoEdge = 1600;
var shrinkAbove = 400 * 1024;
var photoQuality = 0.85;

function canShrink() {
  return !!(window.createImageBitmap && window.Promise && document.createElement("canvas").toBlob);
}

// shrinkPhoto hands back a smaller photo, or the original if it is already
// small enough or anything goes wrong. Reading the file with its own
// orientation means a photo taken sideways is saved the right way up.
function shrinkPhoto(file) {
  return new Promise(function (resolve) {
    if (!file.type || file.type.indexOf("image/") !== 0 || file.size <= shrinkAbove) {
      resolve(file);
      return;
    }
    createImageBitmap(file, { imageOrientation: "from-image" }).then(function (bitmap) {
      var longest = Math.max(bitmap.width, bitmap.height);
      var scale = longest > maxPhotoEdge ? maxPhotoEdge / longest : 1;
      var canvas = document.createElement("canvas");
      canvas.width = Math.round(bitmap.width * scale);
      canvas.height = Math.round(bitmap.height * scale);
      canvas.getContext("2d").drawImage(bitmap, 0, 0, canvas.width, canvas.height);
      bitmap.close();
      canvas.toBlob(function (blob) {
        if (!blob || blob.size >= file.size) {
          resolve(file);
          return;
        }
        resolve(new File([blob], file.name.replace(/\.[^.]+$/, "") + ".jpg", { type: "image/jpeg" }));
      }, "image/jpeg", photoQuality);
    }).catch(function () {
      resolve(file);
    });
  });
}

// shrinkChosen replaces every chosen photo with its smaller copy, in place,
// so the post that follows sends the smaller ones.
function shrinkChosen(form, say) {
  if (!canShrink()) {
    return Promise.resolve();
  }
  var inputs = Array.from(form.querySelectorAll('input[type="file"]'));
  var work = [];
  inputs.forEach(function (input) {
    var files = input.ordered && input.ordered.length ? input.ordered : Array.from(input.files || []);
    if (files.length === 0) {
      return;
    }
    say("Shrinking photos");
    work.push(Promise.all(files.map(shrinkPhoto)).then(function (smaller) {
      input.ordered = smaller;
    }));
  });
  return Promise.all(work);
}

// formPayload gathers a form the way the browser would, except that photos
// go in the order the member arranged them.
function formPayload(form) {
  var data = new FormData();
  form.querySelectorAll("input, textarea, select").forEach(function (field) {
    if (!field.name || field.type === "file") {
      return;
    }
    if (field.type === "checkbox" || field.type === "radio") {
      if (field.checked) {
        data.append(field.name, field.value);
      }
      return;
    }
    data.append(field.name, field.value);
  });
  form.querySelectorAll('input[type="file"]').forEach(function (input) {
    var files = input.ordered && input.ordered.length ? input.ordered : Array.from(input.files || []);
    files.forEach(function (file) {
      data.append(input.name, file, file.name);
    });
  });
  return data;
}

// canSendFromPage reports whether the browser can post the form itself, so
// the decision is made before the submit is stopped.
function canSendFromPage() {
  if (!window.FormData || !window.XMLHttpRequest || !window.Promise) {
    return false;
  }
  return !!new XMLHttpRequest().upload;
}

function sendWithProgress(form, say, done) {
  var request = new XMLHttpRequest();
  request.open(form.method || "post", form.action);
  request.upload.addEventListener("progress", function (event) {
    if (!event.lengthComputable) {
      return;
    }
    var percent = Math.round((event.loaded / event.total) * 100);
    say(percent >= 100 ? afterUploadWord() : "Sending photos " + percent + "%");
  });
  request.upload.addEventListener("load", function () {
    say(afterUploadWord());
  });
  request.addEventListener("load", function () {
    // The handler answers with a redirect both when it takes the build and
    // when it refuses one, so anything in the 200s means we followed a
    // redirect and the address we landed on carries the message. A refused
    // post can land back on the form itself, so the address is not compared.
    if (request.status >= 200 && request.status < 300) {
      if (form.dataset.keep && request.responseURL && request.responseURL !== form.action) {
        clearDraft(form);
      }
      window.location.replace(request.responseURL || form.action);
      return;
    }
    say("Could not save. Reload the page and try again.");
    done();
  });
  request.addEventListener("error", function () {
    say("The connection dropped. Try again.");
    done();
  });
  request.send(formPayload(form));
}

// A part-typed build survives a refresh or a wander off the page. The text
// is kept in this browser only, for a day, and thrown away once the server
// has taken the build. Photos cannot be kept: browsers do not allow a file
// box to be filled by script.
var draftLife = 24 * 60 * 60 * 1000;
var maxDraftBytes = 8192;

function draftKey(form) {
  return "sprue-draft-" + form.dataset.keep;
}

function draftFields(form) {
  return Array.from(form.querySelectorAll("input, textarea, select")).filter(function (field) {
    return field.name && field.type !== "file" && field.type !== "hidden" && field.type !== "submit";
  });
}

function saveDraft(form) {
  var fields = {};
  draftFields(form).forEach(function (field) {
    fields[field.name] = field.type === "checkbox" ? field.checked : field.value;
  });
  var payload = JSON.stringify({ at: Date.now(), fields: fields });
  if (payload.length > maxDraftBytes) {
    return;
  }
  try {
    localStorage.setItem(draftKey(form), payload);
  } catch (e) {
    // A browser with storage turned off simply keeps nothing.
  }
}

function clearDraft(form) {
  try {
    localStorage.removeItem(draftKey(form));
  } catch (e) {
    // Nothing to clear.
  }
}

function readDraft(form) {
  var raw = null;
  try {
    raw = localStorage.getItem(draftKey(form));
  } catch (e) {
    return null;
  }
  if (!raw) {
    return null;
  }
  var saved = null;
  try {
    saved = JSON.parse(raw);
  } catch (e) {
    clearDraft(form);
    return null;
  }
  if (!saved || !saved.fields || Date.now() - saved.at > draftLife) {
    clearDraft(form);
    return null;
  }
  return saved.fields;
}

function restoreDraft(form) {
  var fields = readDraft(form);
  if (!fields) {
    return;
  }
  var filled = 0;
  draftFields(form).forEach(function (field) {
    var value = fields[field.name];
    if (value === undefined) {
      return;
    }
    if (field.type === "checkbox") {
      field.checked = value === true;
      if (field.checked) {
        filled += 1;
      }
      return;
    }
    if (value !== "") {
      field.value = value;
      filled += 1;
    }
  });
  if (filled === 0) {
    clearDraft(form);
    return;
  }
  var notice = document.createElement("p");
  notice.className = "resumed";
  notice.textContent = "Picked up where you left off. Photos need choosing again. ";
  var start = document.createElement("button");
  start.type = "button";
  start.className = "linkish";
  start.textContent = "Start again";
  start.addEventListener("click", function () {
    clearDraft(form);
    form.reset();
    notice.remove();
  });
  notice.appendChild(start);
  form.insertAdjacentElement("afterbegin", notice);
}

document.querySelectorAll("form[data-keep]").forEach(function (form) {
  restoreDraft(form);
  form.addEventListener("input", function () { saveDraft(form); });
  form.addEventListener("change", function () { saveDraft(form); });
});

document.querySelectorAll("form").forEach(function (form) {
  form.addEventListener("submit", function (event) {
    if (form.dataset.busy === "yes") {
      return;
    }
    var buttons = Array.from(form.querySelectorAll("button, input[type=submit]")).filter(function (b) {
      return b.type !== "button";
    });
    if (buttons.length === 0) {
      return;
    }
    form.dataset.busy = "yes";
    var photos = chosenFiles(form);
    var say = showBusy(form, buttons[0], photos > 0 ? "Reading photos" : busyWordFor(buttons[0]));
    var release = function () {
      form.dataset.busy = "no";
      form.removeAttribute("aria-busy");
      buttons.forEach(function (b) { b.disabled = false; });
    };
    // A form that keeps a draft is sent this way too, so the draft is only
    // thrown away once the server has answered that it took the build.
    if ((photos > 0 || form.dataset.keep) && canSendFromPage()) {
      event.preventDefault();
      shrinkChosen(form, say).then(function () {
        sendWithProgress(form, say, release);
      });
    }
    // The form serialises before this runs, so a disabled button's value is
    // still sent. Disabling stops a second press.
    setTimeout(function () {
      buttons.forEach(function (b) { b.disabled = true; });
    }, 0);
  });
});

// A photo opens full size. Escape or a click anywhere closes it, and the
// button that opened it takes the focus back.
function photoViewer() {
  var shots = document.querySelectorAll(".gallery .shot");
  if (shots.length === 0) {
    return;
  }
  var frame = document.createElement("div");
  frame.className = "viewer";
  frame.hidden = true;
  var full = document.createElement("img");
  full.alt = "";
  var close = document.createElement("button");
  close.type = "button";
  close.className = "nes-btn close";
  close.textContent = "Close";
  frame.append(full, close);
  document.body.appendChild(frame);

  var opener = null;
  var shut = function () {
    frame.hidden = true;
    full.removeAttribute("src");
    document.body.classList.remove("viewing");
    if (opener) {
      opener.focus();
      opener = null;
    }
  };
  frame.addEventListener("click", shut);
  document.addEventListener("keydown", function (event) {
    if (event.key === "Escape" && !frame.hidden) {
      shut();
    }
  });
  shots.forEach(function (shot) {
    shot.addEventListener("click", function () {
      var image = shot.querySelector("img");
      if (!image) {
        return;
      }
      opener = shot;
      full.src = image.src;
      full.alt = image.alt;
      frame.hidden = false;
      document.body.classList.add("viewing");
      close.focus();
    });
  });
}

photoViewer();
