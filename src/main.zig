const std = @import("std");
const Io = std.Io;
const assert = std.debug.assert;
const sprue = @import("sprue");
const content = sprue.content;
const html = sprue.html;

const arena_bytes = 64 << 20;
const max_photos_per_build = 256;
const max_directory_entries = 4096;
const style_css = @embedFile("assets/style.css");
const spin_js = @embedFile("assets/spin.js");

const BuildEntry = struct {
    build: content.Build,
    spin_frames: usize,
};

pub fn main(init: std.process.Init) !void {
    const backing = init.arena.allocator().alloc(u8, arena_bytes) catch {
        std.debug.print("sprue: cannot reserve {d} bytes of memory at startup\n", .{arena_bytes});
        return error.OutOfMemory;
    };
    assert(backing.len == arena_bytes);
    var fixed = std.heap.FixedBufferAllocator.init(backing);
    run(init.io, fixed.allocator()) catch |err| {
        std.debug.print("sprue: failed: {s}\n", .{@errorName(err)});
        return err;
    };
    assert(fixed.end_index <= arena_bytes);
}

fn run(io: Io, arena: std.mem.Allocator) !void {
    var content_dir = Io.Dir.cwd().openDir(io, "content", .{ .iterate = true }) catch {
        std.debug.print("sprue: no content directory here; create content/<build-name>/build.txt first\n", .{});
        return error.NoContentDirectory;
    };
    defer content_dir.close(io);

    const builds = try arena.alloc(BuildEntry, content.max_builds);
    var build_count: usize = 0;
    var entries_seen: usize = 0;
    var directories = content_dir.iterate();
    while (try directories.next(io)) |entry| {
        entries_seen += 1;
        if (entries_seen > max_directory_entries) return error.TooManyEntries;
        if (entry.kind != .directory) continue;
        if (build_count == content.max_builds) return error.TooManyBuilds;
        builds[build_count] = try loadBuild(io, arena, content_dir, entry.name);
        build_count += 1;
    }
    if (build_count == 0) return error.NoBuildsFound;
    assert(build_count <= content.max_builds);
    std.mem.sort(BuildEntry, builds[0..build_count], {}, buildBeforeBuild);
    try writeSite(io, arena, content_dir, builds[0..build_count]);
    std.debug.print("sprue: generated docs/ with {d} build(s)\n", .{build_count});
}

fn loadBuild(io: Io, arena: std.mem.Allocator, content_dir: Io.Dir, name: []const u8) !BuildEntry {
    assert(name.len > 0);
    var build_dir = try content_dir.openDir(io, name, .{});
    defer build_dir.close(io);
    const slug = try arena.dupe(u8, name);
    const text = build_dir.readFileAlloc(io, "build.txt", arena, .limited(content.max_file_bytes)) catch |err| {
        std.debug.print("sprue: cannot read content/{s}/build.txt: {s}\n", .{ name, @errorName(err) });
        return err;
    };
    const build = content.parse(arena, slug, text) catch |err| {
        std.debug.print("sprue: content/{s}/build.txt: {s}\n", .{ name, @errorName(err) });
        return err;
    };
    try validatePhotos(io, build_dir, build);
    const spin_frames = try countSpinFrames(io, build_dir, name);
    assert(build.slug.len > 0);
    return .{ .build = build, .spin_frames = spin_frames };
}

fn validatePhotos(io: Io, build_dir: Io.Dir, build: content.Build) !void {
    assert(build.kit.len > 0);
    var checked: usize = 0;
    for (build.stages) |stage| {
        for (stage.items) |item| switch (item) {
            .photo => |file| {
                checked += 1;
                assert(checked <= content.max_stages * content.max_items_per_stage);
                build_dir.access(io, file, .{}) catch {
                    std.debug.print("sprue: photo {s} listed in {s}/build.txt but the file is missing\n", .{ file, build.slug });
                    return error.MissingPhoto;
                };
            },
            .paragraph => {},
        };
    }
}

fn countSpinFrames(io: Io, build_dir: Io.Dir, name: []const u8) !usize {
    assert(name.len > 0);
    var spin_dir = build_dir.openDir(io, "spin", .{}) catch return 0;
    defer spin_dir.close(io);
    var frames: usize = 0;
    while (frames < html.max_spin_frames) : (frames += 1) {
        var frame_name_bytes: [16]u8 = undefined;
        const frame_name = spinFrameName(&frame_name_bytes, frames + 1);
        spin_dir.access(io, frame_name, .{}) catch break;
    }
    if (frames == 0) {
        std.debug.print("sprue: content/{s}/spin/ exists but has no spin_01.jpg\n", .{name});
        return error.EmptySpinDirectory;
    }
    assert(frames <= html.max_spin_frames);
    return frames;
}

fn spinFrameName(buffer: *[16]u8, frame: usize) []const u8 {
    assert(frame >= 1);
    assert(frame <= html.max_spin_frames);
    return std.fmt.bufPrint(buffer, "spin_{d:0>2}.jpg", .{frame}) catch unreachable;
}

fn writeSite(io: Io, arena: std.mem.Allocator, content_dir: Io.Dir, builds: []const BuildEntry) !void {
    assert(builds.len > 0);
    assert(builds.len <= content.max_builds);
    try Io.Dir.cwd().createDirPath(io, "docs");
    var docs_dir = try Io.Dir.cwd().openDir(io, "docs", .{});
    defer docs_dir.close(io);
    try docs_dir.writeFile(io, .{ .sub_path = "style.css", .data = style_css });
    try docs_dir.writeFile(io, .{ .sub_path = "spin.js", .data = spin_js });

    const page_buffer = try arena.alloc(u8, html.max_page_bytes);
    const plain = try arena.alloc(content.Build, builds.len);
    for (builds, 0..) |entry, index| plain[index] = entry.build;

    var sink = html.Sink.init(page_buffer);
    try html.renderIndex(&sink, plain);
    try docs_dir.writeFile(io, .{ .sub_path = "index.html", .data = sink.view() });

    for (builds) |entry| {
        try writeBuildPage(io, page_buffer, docs_dir, entry);
        try copyBuildFiles(io, content_dir, docs_dir, entry);
    }
}

fn writeBuildPage(io: Io, page_buffer: []u8, docs_dir: Io.Dir, entry: BuildEntry) !void {
    assert(entry.build.slug.len > 0);
    var sink = html.Sink.init(page_buffer);
    try html.renderBuildPage(&sink, entry.build, entry.spin_frames);
    try docs_dir.createDirPath(io, entry.build.slug);
    var out_dir = try docs_dir.openDir(io, entry.build.slug, .{});
    defer out_dir.close(io);
    try out_dir.writeFile(io, .{ .sub_path = "index.html", .data = sink.view() });
    assert(sink.len > 0);
}

fn copyBuildFiles(io: Io, content_dir: Io.Dir, docs_dir: Io.Dir, entry: BuildEntry) !void {
    assert(entry.build.slug.len > 0);
    var build_dir = try content_dir.openDir(io, entry.build.slug, .{ .iterate = true });
    defer build_dir.close(io);
    var out_dir = try docs_dir.openDir(io, entry.build.slug, .{});
    defer out_dir.close(io);
    var copied: usize = 0;
    var seen: usize = 0;
    var files = build_dir.iterate();
    while (try files.next(io)) |file_entry| {
        seen += 1;
        if (seen > max_directory_entries) return error.TooManyEntries;
        if (file_entry.kind != .file) continue;
        if (!isPhotoName(file_entry.name)) continue;
        if (copied == max_photos_per_build) return error.TooManyPhotos;
        try build_dir.copyFile(file_entry.name, out_dir, file_entry.name, io, .{});
        copied += 1;
    }
    assert(copied <= max_photos_per_build);
    if (entry.spin_frames > 0) try copySpinFrames(io, build_dir, out_dir, entry.spin_frames);
}

fn copySpinFrames(io: Io, build_dir: Io.Dir, out_dir: Io.Dir, frames: usize) !void {
    assert(frames >= 1);
    assert(frames <= html.max_spin_frames);
    var spin_in = try build_dir.openDir(io, "spin", .{});
    defer spin_in.close(io);
    try out_dir.createDirPath(io, "spin");
    var spin_out = try out_dir.openDir(io, "spin", .{});
    defer spin_out.close(io);
    var index: usize = 0;
    while (index < frames) : (index += 1) {
        var frame_name_bytes: [16]u8 = undefined;
        const frame_name = spinFrameName(&frame_name_bytes, index + 1);
        try spin_in.copyFile(frame_name, spin_out, frame_name, io, .{});
    }
}

fn isPhotoName(name: []const u8) bool {
    assert(name.len > 0);
    const extensions = [_][]const u8{ ".jpg", ".jpeg", ".png", ".webp", ".gif" };
    for (extensions) |extension| {
        assert(extension[0] == '.');
        if (std.ascii.endsWithIgnoreCase(name, extension)) return true;
    }
    return false;
}

fn buildBeforeBuild(context: void, a: BuildEntry, b: BuildEntry) bool {
    _ = context;
    assert(a.build.slug.len > 0);
    assert(b.build.slug.len > 0);
    return std.mem.lessThan(u8, a.build.slug, b.build.slug);
}
