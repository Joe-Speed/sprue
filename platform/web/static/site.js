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
