"use strict";

function frameSource(frame) {
  var padded = String(frame).padStart(2, "0");
  return "spin/spin_" + padded + ".jpg";
}

function setupSpin(container) {
  var count = Number(container.dataset.frames);
  if (!Number.isInteger(count) || count < 1 || count > 99) {
    return;
  }
  var image = container.querySelector("img");
  if (image === null) {
    return;
  }
  var frame = 1;
  var dragging = false;
  var lastX = 0;
  var pixelsPerFrame = 6;

  for (var preload = 1; preload <= count; preload += 1) {
    new Image().src = frameSource(preload);
  }

  function show(next) {
    frame = ((next - 1) % count + count) % count + 1;
    image.src = frameSource(frame);
  }

  container.addEventListener("pointerdown", function (event) {
    dragging = true;
    lastX = event.clientX;
    container.setPointerCapture(event.pointerId);
  });

  container.addEventListener("pointermove", function (event) {
    if (!dragging) {
      return;
    }
    var moved = event.clientX - lastX;
    if (Math.abs(moved) >= pixelsPerFrame) {
      show(frame + Math.sign(moved));
      lastX = event.clientX;
    }
  });

  container.addEventListener("pointerup", function () {
    dragging = false;
  });

  container.addEventListener("pointercancel", function () {
    dragging = false;
  });
}

document.querySelectorAll(".spin").forEach(setupSpin);
