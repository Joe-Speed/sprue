const std = @import("std");
const assert = std.debug.assert;

pub const max_builds = 128;
pub const max_stages = 32;
pub const max_items_per_stage = 128;
pub const max_file_bytes = 1 << 20;

pub const Item = union(enum) {
    paragraph: []const u8,
    photo: []const u8,
};

pub const Stage = struct {
    title: []const u8,
    items: []const Item,
};

pub const Build = struct {
    slug: []const u8,
    kit: []const u8 = "",
    brand: []const u8 = "",
    scale: []const u8 = "",
    status: []const u8 = "",
    started: []const u8 = "",
    finished: []const u8 = "",
    next: []const u8 = "",
    stages: []const Stage = &.{},
};

pub const ParseError = error{
    MissingKitLine,
    TooManyStages,
    TooManyItemsInStage,
    ContentBeforeFirstStage,
    OutOfMemory,
};

const StageBuilder = struct {
    title: []const u8,
    items: []Item,
    count: usize,
};

pub fn parse(arena: std.mem.Allocator, slug: []const u8, text: []const u8) ParseError!Build {
    assert(slug.len > 0);
    assert(text.len <= max_file_bytes);

    var build = Build{ .slug = slug };
    const stages = try arena.alloc(Stage, max_stages);
    var stage_count: usize = 0;
    var current: ?StageBuilder = null;

    var lines = std.mem.splitScalar(u8, text, '\n');
    var lines_seen: usize = 0;
    while (lines.next()) |raw| {
        lines_seen += 1;
        assert(lines_seen <= max_file_bytes + 1);
        const line = std.mem.trim(u8, raw, " \t\r");
        if (line.len == 0) continue;
        if (std.mem.startsWith(u8, line, "## ")) {
            if (current) |done| {
                if (stage_count == max_stages) return error.TooManyStages;
                stages[stage_count] = .{ .title = done.title, .items = done.items[0..done.count] };
                stage_count += 1;
            }
            current = .{
                .title = std.mem.trim(u8, line[3..], " \t"),
                .items = try arena.alloc(Item, max_items_per_stage),
                .count = 0,
            };
        } else if (current) |*open| {
            if (open.count == max_items_per_stage) return error.TooManyItemsInStage;
            open.items[open.count] = itemFromLine(line);
            open.count += 1;
        } else if (!applyHeaderLine(&build, line)) {
            return error.ContentBeforeFirstStage;
        }
    }
    if (current) |done| {
        if (stage_count == max_stages) return error.TooManyStages;
        stages[stage_count] = .{ .title = done.title, .items = done.items[0..done.count] };
        stage_count += 1;
    }
    if (build.kit.len == 0) return error.MissingKitLine;
    build.stages = stages[0..stage_count];
    return build;
}

fn itemFromLine(line: []const u8) Item {
    assert(line.len > 0);
    assert(!std.mem.startsWith(u8, line, "## "));
    if (std.mem.startsWith(u8, line, "photo:")) {
        return .{ .photo = std.mem.trim(u8, line["photo:".len..], " \t") };
    }
    return .{ .paragraph = line };
}

fn applyHeaderLine(build: *Build, line: []const u8) bool {
    assert(line.len > 0);
    assert(!std.mem.startsWith(u8, line, "## "));
    if (headerValue(line, "kit:")) |value| {
        build.kit = value;
    } else if (headerValue(line, "brand:")) |value| {
        build.brand = value;
    } else if (headerValue(line, "scale:")) |value| {
        build.scale = value;
    } else if (headerValue(line, "status:")) |value| {
        build.status = value;
    } else if (headerValue(line, "started:")) |value| {
        build.started = value;
    } else if (headerValue(line, "finished:")) |value| {
        build.finished = value;
    } else if (headerValue(line, "next:")) |value| {
        build.next = value;
    } else {
        return false;
    }
    return true;
}

fn headerValue(line: []const u8, key: []const u8) ?[]const u8 {
    assert(key.len > 1);
    assert(key[key.len - 1] == ':');
    if (!std.mem.startsWith(u8, line, key)) return null;
    return std.mem.trim(u8, line[key.len..], " \t");
}

const testing = std.testing;

test "parses a full build file" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const text =
        \\kit: Airfix 1/72 Spitfire Mk.I
        \\brand: Airfix
        \\scale: 1/72
        \\status: finished
        \\
        \\## Unboxing
        \\Crisp moulding, no flash on the sprues.
        \\photo: box.jpg
        \\
        \\## Painting
        \\First coat of interior green on the cockpit.
    ;
    const build = try parse(arena_state.allocator(), "spitfire-mk1", text);
    try testing.expectEqualStrings("Airfix 1/72 Spitfire Mk.I", build.kit);
    try testing.expectEqualStrings("Airfix", build.brand);
    try testing.expectEqualStrings("finished", build.status);
    try testing.expectEqual(@as(usize, 2), build.stages.len);
    try testing.expectEqualStrings("Unboxing", build.stages[0].title);
    try testing.expectEqual(@as(usize, 2), build.stages[0].items.len);
    try testing.expectEqualStrings("box.jpg", build.stages[0].items[1].photo);
    try testing.expectEqualStrings("Painting", build.stages[1].title);
}

test "missing kit line is an error" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const text = "## Stage\nSome text.";
    try testing.expectError(error.MissingKitLine, parse(arena_state.allocator(), "x", text));
}

test "content before the first stage is an error" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const text = "kit: K\nThis line belongs to no stage.";
    try testing.expectError(error.ContentBeforeFirstStage, parse(arena_state.allocator(), "x", text));
}

test "header lines may appear in any order and unknown keys fail loudly" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const text = "scale: 1/48\nkit: Tamiya Mustang\nmystery: value";
    try testing.expectError(error.ContentBeforeFirstStage, parse(arena_state.allocator(), "x", text));
}

test "a stashed kit needs only a kit line and can carry a next step" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const text = "kit: Redemptor Dreadnought\nbrand: Games Workshop\nstatus: in stash\nnext: clip and clean the torso parts, Saturday morning";
    const build = try parse(arena_state.allocator(), "dreadnought", text);
    try testing.expectEqualStrings("in stash", build.status);
    try testing.expectEqualStrings("clip and clean the torso parts, Saturday morning", build.next);
    try testing.expectEqual(@as(usize, 0), build.stages.len);
}
