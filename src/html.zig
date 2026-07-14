const std = @import("std");
const assert = std.debug.assert;
const content = @import("content.zig");

pub const max_page_bytes = 1 << 20;
pub const max_spin_frames = 99;

pub const Sink = struct {
    buffer: []u8,
    len: usize,

    pub fn init(buffer: []u8) Sink {
        assert(buffer.len >= 1024);
        assert(buffer.len <= max_page_bytes);
        return .{ .buffer = buffer, .len = 0 };
    }

    pub fn add(sink: *Sink, text: []const u8) error{PageTooLarge}!void {
        assert(sink.len <= sink.buffer.len);
        if (text.len > sink.buffer.len - sink.len) return error.PageTooLarge;
        @memcpy(sink.buffer[sink.len..][0..text.len], text);
        sink.len += text.len;
        assert(sink.len <= sink.buffer.len);
    }

    pub fn view(sink: *const Sink) []const u8 {
        assert(sink.len <= sink.buffer.len);
        assert(sink.buffer.len > 0);
        return sink.buffer[0..sink.len];
    }
};

pub fn addEscaped(sink: *Sink, text: []const u8) error{PageTooLarge}!void {
    assert(sink.len <= sink.buffer.len);
    var index: usize = 0;
    while (index < text.len) : (index += 1) {
        switch (text[index]) {
            '&' => try sink.add("&amp;"),
            '<' => try sink.add("&lt;"),
            '>' => try sink.add("&gt;"),
            '"' => try sink.add("&quot;"),
            else => try sink.add(text[index .. index + 1]),
        }
    }
    assert(index == text.len);
}

pub fn renderIndex(sink: *Sink, builds: []const content.Build) error{PageTooLarge}!void {
    assert(builds.len > 0);
    assert(builds.len <= content.max_builds);
    try pageOpen(sink, "Build log", "");
    try sink.add("<h1>Build log</h1>\n");
    try renderStashSummary(sink, builds);
    try sink.add("<ul class=\"builds\">\n");
    for (builds) |build| {
        try sink.add("<li><a href=\"");
        try addEscaped(sink, build.slug);
        try sink.add("/\"><strong>");
        try addEscaped(sink, build.kit);
        try sink.add("</strong>");
        if (build.status.len > 0) {
            try sink.add(" <em>");
            try addEscaped(sink, build.status);
            try sink.add("</em>");
        }
        try sink.add("</a></li>\n");
    }
    try sink.add("</ul>\n");
    try pageClose(sink);
}

pub fn renderBuildPage(sink: *Sink, build: content.Build, spin_frames: usize) error{PageTooLarge}!void {
    assert(build.kit.len > 0);
    assert(spin_frames <= max_spin_frames);
    try pageOpen(sink, build.kit, "../");
    try sink.add("<p class=\"back\"><a href=\"../\">All builds</a></p>\n<h1>");
    try addEscaped(sink, build.kit);
    try sink.add("</h1>\n");
    try renderMeta(sink, build);
    if (build.next.len > 0) {
        try sink.add("<p class=\"next\">Next step: ");
        try addEscaped(sink, build.next);
        try sink.add("</p>\n");
    }
    if (spin_frames > 0) try renderSpin(sink, spin_frames);
    for (build.stages) |stage| {
        try sink.add("<h2>");
        try addEscaped(sink, stage.title);
        try sink.add("</h2>\n");
        for (stage.items) |item| switch (item) {
            .paragraph => |text| {
                try sink.add("<p>");
                try addEscaped(sink, text);
                try sink.add("</p>\n");
            },
            .photo => |file| {
                try sink.add("<img src=\"");
                try addEscaped(sink, file);
                try sink.add("\" alt=\"");
                try addEscaped(sink, stage.title);
                try sink.add("\" loading=\"lazy\">\n");
            },
        };
    }
    try pageClose(sink);
}

fn renderMeta(sink: *Sink, build: content.Build) error{PageTooLarge}!void {
    assert(build.kit.len > 0);
    const fields = [_]struct { label: []const u8, value: []const u8 }{
        .{ .label = "Brand", .value = build.brand },
        .{ .label = "Scale", .value = build.scale },
        .{ .label = "Status", .value = build.status },
        .{ .label = "Started", .value = build.started },
        .{ .label = "Finished", .value = build.finished },
    };
    try sink.add("<p class=\"meta\">");
    var wrote_any = false;
    for (fields) |field| {
        if (field.value.len == 0) continue;
        if (wrote_any) try sink.add(" · ");
        try sink.add(field.label);
        try sink.add(": ");
        try addEscaped(sink, field.value);
        wrote_any = true;
    }
    try sink.add("</p>\n");
    assert(sink.len > 0);
}

fn renderStashSummary(sink: *Sink, builds: []const content.Build) error{PageTooLarge}!void {
    assert(builds.len > 0);
    assert(builds.len <= content.max_builds);
    var finished: usize = 0;
    var in_progress: usize = 0;
    var in_stash: usize = 0;
    for (builds) |build| {
        if (std.ascii.eqlIgnoreCase(build.status, "finished")) finished += 1;
        if (std.ascii.eqlIgnoreCase(build.status, "in progress")) in_progress += 1;
        if (std.ascii.eqlIgnoreCase(build.status, "in stash")) in_stash += 1;
    }
    if (finished + in_progress + in_stash == 0) return;
    try sink.add("<p class=\"stash\">");
    try addNumber(sink, finished);
    try sink.add(" finished · ");
    try addNumber(sink, in_progress);
    try sink.add(" in progress · ");
    try addNumber(sink, in_stash);
    try sink.add(" in the stash</p>\n");
}

fn renderSpin(sink: *Sink, frames: usize) error{PageTooLarge}!void {
    assert(frames >= 1);
    assert(frames <= max_spin_frames);
    try sink.add("<div class=\"spin\" data-frames=\"");
    try addNumber(sink, frames);
    try sink.add("\">\n<img src=\"spin/spin_01.jpg\" alt=\"360 degree view\" draggable=\"false\">\n<p>drag to rotate</p>\n</div>\n<script src=\"../spin.js\"></script>\n");
}

fn addNumber(sink: *Sink, value: usize) error{PageTooLarge}!void {
    assert(value <= 9999);
    var digits: [4]u8 = undefined;
    const text = std.fmt.bufPrint(&digits, "{d}", .{value}) catch unreachable;
    assert(text.len >= 1);
    try sink.add(text);
}

fn pageOpen(sink: *Sink, title: []const u8, root_prefix: []const u8) error{PageTooLarge}!void {
    assert(title.len > 0);
    assert(root_prefix.len <= 3);
    try sink.add("<!doctype html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n<title>");
    try addEscaped(sink, title);
    try sink.add("</title>\n<link rel=\"stylesheet\" href=\"");
    try sink.add(root_prefix);
    try sink.add("style.css\">\n</head>\n<body>\n<main>\n");
}

fn pageClose(sink: *Sink) error{PageTooLarge}!void {
    assert(sink.len > 0);
    try sink.add("</main>\n</body>\n</html>\n");
    assert(sink.len > 40);
}

const testing = std.testing;

test "sink refuses to overflow its buffer" {
    var buffer: [1024]u8 = undefined;
    var sink = Sink.init(&buffer);
    const chunk = "x" ** 1024;
    try sink.add(chunk);
    try testing.expectError(error.PageTooLarge, sink.add("y"));
    try testing.expectEqual(@as(usize, 1024), sink.len);
}

test "escaping covers the html special characters" {
    var buffer: [1024]u8 = undefined;
    var sink = Sink.init(&buffer);
    try addEscaped(&sink, "a<b & \"c\">");
    try testing.expectEqualStrings("a&lt;b &amp; &quot;c&quot;&gt;", sink.view());
}

test "index page links every build" {
    var buffer: [65536]u8 = undefined;
    var sink = Sink.init(&buffer);
    const builds = [_]content.Build{
        .{ .slug = "spitfire-mk1", .kit = "Airfix 1/72 Spitfire Mk.I", .status = "finished" },
        .{ .slug = "tiger-i", .kit = "Tamiya 1/35 Tiger I" },
    };
    try renderIndex(&sink, &builds);
    try testing.expect(std.mem.indexOf(u8, sink.view(), "href=\"spitfire-mk1/\"") != null);
    try testing.expect(std.mem.indexOf(u8, sink.view(), "Tamiya 1/35 Tiger I") != null);
}

test "index page counts the stash" {
    var buffer: [65536]u8 = undefined;
    var sink = Sink.init(&buffer);
    const builds = [_]content.Build{
        .{ .slug = "a", .kit = "Kit A", .status = "finished" },
        .{ .slug = "b", .kit = "Kit B", .status = "In Stash" },
        .{ .slug = "c", .kit = "Kit C", .status = "in stash" },
    };
    try renderIndex(&sink, &builds);
    try testing.expect(std.mem.indexOf(u8, sink.view(), "1 finished · 0 in progress · 2 in the stash") != null);
}

test "build page shows the next step when one is set" {
    var buffer: [65536]u8 = undefined;
    var sink = Sink.init(&buffer);
    const build = content.Build{ .slug = "d", .kit = "Kit D", .next = "mask the canopy" };
    try renderBuildPage(&sink, build, 0);
    try testing.expect(std.mem.indexOf(u8, sink.view(), "Next step: mask the canopy") != null);
}

test "build page includes the spin viewer only when frames exist" {
    var buffer: [65536]u8 = undefined;
    var sink = Sink.init(&buffer);
    const build = content.Build{ .slug = "spitfire-mk1", .kit = "Airfix Spitfire" };
    try renderBuildPage(&sink, build, 36);
    try testing.expect(std.mem.indexOf(u8, sink.view(), "data-frames=\"36\"") != null);

    sink = Sink.init(&buffer);
    try renderBuildPage(&sink, build, 0);
    try testing.expect(std.mem.indexOf(u8, sink.view(), "data-frames") == null);
}
