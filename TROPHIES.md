# Designing the trophies

Winners download a 3D-printable trophy that arrives as a miniature kit: a small sprue frame with the trophy parts attached by thin gates. You snip the parts off and assemble them like any model. The three files are first-place.stl, second-place.stl, and third-place.stl. They live only in the server's data directory at `data/stl/`, never in this repo, and the site hands each one out once to its winner.

## The tool

Fusion 360 with the personal use licence. It is free for hobby work, runs on Mac and Windows, and is parametric, which means the frame, the gates, and the cups are dimensions you can change later without redrawing. Text is a proper feature, so a placing number or a year on the plaque is a few clicks. It exports STL directly.

Learn it in this order:

1. Sketch and extrude. A rectangle becomes the base plaque. A circle becomes the stem. This is most of what a trophy needs.
2. Revolve. Draw half a cup profile and revolve it around its axis. This is the cup body in one operation.
3. Combine. Join the cup to the stem and base, cut a hollow into the cup.
4. Parameters. Give the runner width, gate width, and cup height names. Change them once, everything follows.
5. Components and patterns. Make the frame one component and the parts others, then arrange the parts on the frame.
6. Text on a face, then export as STL in millimetres.

Autodesk's own beginner series covers steps 1 to 4 in about two hours. Expect a weekend to a first printable sprue.

You also need a slicer for your printer, which is free: PrusaSlicer, Bambu Studio, or Cura. It is where you check that gates are printable and see the part the way a winner will.

## The design

Model in millimetres. STL files carry no units and every slicer assumes millimetres.

The frame is a rectangle of runner, 2.5 to 3 millimetres square in section, sized to fit a small print bed with room to spare. Around 80 by 60 millimetres is a good start.

Gates connect each part to the runner. Make them 0.8 to 1.2 millimetres thick so sprue cutters snip them cleanly, and attach them where a cut mark will not show, such as under a base or behind a stem.

The parts for one trophy: a base plaque, a stem, a cup, and two handles. Keep every part flat side down on the frame so it prints without supports, which is the same reason real kit parts are shaped the way they are. Add a locating peg on the stem and a matching hole in the base and cup so assembly needs no guesswork.

Leave one flat face on the plaque for text. The placing goes there. A year or a name can go there later for a personalised version.

First, second, and third share the frame and differ in the cup. A taller cup with two handles for first, a plain cup for second, a shorter one for third. The plaque text changes for each. Winners paint them: gold DB0016 for first, silver DB0011 for second, antique bronze DB0171 for third, so the printed colour does not matter.

## Printing

Resin gives the crisp detail a small trophy wants and handles thin gates well. If you print on FDM, thicken the gates to 1.2 millimetres and avoid features under a millimetre.

Print the frame flat on the bed. Check the sliced preview for any part that lifts off the frame, and check that no gate is thinner than your nozzle or pixel size.

Print one test sprue before designing the three finals. It tells you the right gate thickness for your printer in an afternoon.

## Handing the files to the site

Export each trophy as one STL containing the whole sprue. Name them exactly first-place.stl, second-place.stl, and third-place.stl and copy them into `data/stl/` on the server volume. Nothing else is needed; the download links appear on winners' profiles as soon as a competition is decided.

Keep the Fusion source files somewhere private, not in this repository. The repository is public and the trophies are meant to be earned.
