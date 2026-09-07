// Shows the photos a member has chosen before they upload them.
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
