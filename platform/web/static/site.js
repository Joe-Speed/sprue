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

// Chosen photos show as thumbnails before they upload.
document.querySelectorAll('input[type="file"][accept^="image"]').forEach(function (input) {
  var strip = document.createElement("div");
  strip.className = "previews";
  input.insertAdjacentElement("afterend", strip);
  input.addEventListener("change", function () {
    strip.replaceChildren();
    Array.from(input.files).slice(0, 6).forEach(function (file) {
      var image = document.createElement("img");
      image.alt = file.name;
      image.src = URL.createObjectURL(file);
      image.onload = function () { URL.revokeObjectURL(image.src); };
      strip.appendChild(image);
    });
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
    count += input.files ? input.files.length : 0;
  });
  return count;
}

// sendWithProgress posts a form with photos through XMLHttpRequest so the
// strip can say what is really happening: reading the files, sending the
// bytes, then waiting while the server saves. A plain form post gives no
// such signal. Anything unsupported falls back to the normal submit.
function sendWithProgress(form, say, done) {
  if (!window.FormData || !window.XMLHttpRequest) {
    return false;
  }
  var request = new XMLHttpRequest();
  if (!request.upload) {
    return false;
  }
  request.open(form.method || "post", form.action);
  request.upload.addEventListener("progress", function (event) {
    if (!event.lengthComputable) {
      return;
    }
    var percent = Math.round((event.loaded / event.total) * 100);
    say(percent >= 100 ? "Saving" : "Sending photos " + percent + "%");
  });
  request.upload.addEventListener("load", function () {
    say("Saving");
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
  request.send(new FormData(form));
  return true;
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
    if ((photos > 0 || form.dataset.keep) && sendWithProgress(form, say, release)) {
      event.preventDefault();
    }
    // The form serialises before this runs, so a disabled button's value is
    // still sent. Disabling stops a second press.
    setTimeout(function () {
      buttons.forEach(function (b) { b.disabled = true; });
    }, 0);
  });
});
