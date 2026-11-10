// y-shelf: a shelf at the right edge of the screen. Files given on the command
// line, or dropped onto it, wait there until you drag them out into a browser,
// Finder or a chat app. Marmota's Y runs it; build.sh builds it into bin/.
//
//   swiftc -O main.swift -o y-shelf && ./y-shelf a.pdf b.png &
//
// While a shelf is open, running y-shelf again adds the files to it.
//
// The files that come in together (one Y, one drop) sit in one pile. Each
// pile: drag it out whole (it leaves the shelf unless locked), double-click to
// open it (a pile of several: show it in Finder), x to remove it, the eye or
// space to Quick Look it, the lock to keep it after a drop. The shelf quits
// when it empties, or on esc.
import AppKit
import Quartz
import QuickLookThumbnailing

let panelWidth: CGFloat = 116
let rowHeight: CGFloat = 104
let buttonColumn: CGFloat = 28
let footHeight: CGFloat = 32
// See-through until the pointer is on it, or files are dragged over it.
let restingAlpha: CGFloat = 0.92

// Item is one pile: the files that came in together.
final class Item {
    let urls: [URL]
    var locked = false
    var thumbs: [NSImage]
    weak var view: NSView?

    init(urls: [URL]) {
        self.urls = urls
        thumbs = urls.map { NSWorkspace.shared.icon(forFile: $0.path) }
    }

    var title: String { urls.count == 1 ? urls[0].lastPathComponent : "\(urls.count) files" }

    // The pill's text: the names, the first few of a big pile.
    var names: String {
        let shown = urls.prefix(3).map(\.lastPathComponent).joined(separator: ", ")
        return urls.count > 3 ? "\(shown) +\(urls.count - 3)" : shown
    }
}

final class Shelf {
    static let shared = Shelf()
    var items: [Item] = []
    weak var hovered: Item?
    var onChange: () -> Void = {}
    private var everFilled = false

    // add puts urls on the shelf as one pile, leaving out the ones already on it.
    func add(_ urls: [URL]) {
        var fresh: [URL] = []
        for url in urls where !fresh.contains(url) && !items.contains(where: { $0.urls.contains(url) }) {
            fresh.append(url)
        }
        if !fresh.isEmpty {
            let item = Item(urls: fresh)
            items.append(item)
            everFilled = true
            // Only the top three show in the pile.
            for i in fresh.indices.prefix(3) { loadThumbnail(item, i) }
        }
        onChange()
    }

    func remove(_ item: Item) {
        items.removeAll { $0 === item }
        changed()
    }

    func clear() {
        items.removeAll()
        changed()
    }

    private func changed() {
        if items.isEmpty && everFilled { NSApp.terminate(nil) }
        onChange()
    }

    // Quick Look's picture of the file: a page of a PDF, a text file's first
    // lines. The Finder icon stands in until it arrives.
    private func loadThumbnail(_ item: Item, _ i: Int) {
        let request = QLThumbnailGenerator.Request(
            fileAt: item.urls[i], size: CGSize(width: 128, height: 160), scale: 2, representationTypes: .all)
        QLThumbnailGenerator.shared.generateBestRepresentation(for: request) { rep, _ in
            guard let image = rep?.nsImage else { return }
            DispatchQueue.main.async {
                item.thumbs[i] = image
                item.view?.needsDisplay = true
            }
        }
    }
}

func symbolButton(_ name: String, size: CGFloat, target: AnyObject, action: Selector) -> NSButton {
    let config = NSImage.SymbolConfiguration(pointSize: size, weight: .regular)
    let image = NSImage(systemSymbolName: name, accessibilityDescription: name)!.withSymbolConfiguration(config)!
    let button = NSButton(image: image, target: target, action: action)
    button.isBordered = false
    button.imagePosition = .imageOnly
    button.refusesFirstResponder = true // no focus ring
    button.contentTintColor = NSColor(white: 1, alpha: 0.8)
    return button
}

// The file's name, in a pill to the left of the shelf while the pointer is on it.
final class NamePill {
    let window = NSWindow(contentRect: .zero, styleMask: .borderless, backing: .buffered, defer: true)
    let field = NSTextField(labelWithString: "")

    init() {
        window.isOpaque = false
        window.backgroundColor = .clear
        window.hasShadow = true
        window.level = .floating
        window.ignoresMouseEvents = true
        let background = NSView()
        background.wantsLayer = true
        background.layer?.backgroundColor = NSColor(white: 0.13, alpha: 0.95).cgColor
        background.layer?.cornerRadius = 8
        background.layer?.borderWidth = 0.5
        background.layer?.borderColor = NSColor(white: 1, alpha: 0.15).cgColor
        field.font = .systemFont(ofSize: 13, weight: .medium)
        field.textColor = .white
        field.lineBreakMode = .byTruncatingMiddle
        background.addSubview(field)
        window.contentView = background
    }

    func show(_ text: String, rightEdge: CGFloat, centerY: CGFloat) {
        field.stringValue = text
        field.sizeToFit()
        let width = min(field.frame.width + 20, 360)
        let height: CGFloat = 28
        field.frame = NSRect(x: 10, y: (height - field.frame.height) / 2, width: width - 20, height: field.frame.height)
        window.setFrame(NSRect(x: rightEdge - width - 6, y: centerY - height / 2, width: width, height: height), display: true)
        window.orderFront(nil)
    }

    func hide() { window.orderOut(nil) }
}

let namePill = NamePill()

final class FlippedView: NSView {
    override var isFlipped: Bool { true }
}

final class ItemView: NSView, NSDraggingSource {
    let item: Item
    var closeButton: NSButton!
    var eyeButton: NSButton!
    var lockButton: NSButton!
    var hovering = false {
        didSet {
            updateButtons()
            needsDisplay = true
        }
    }
    var mouseDownAt: NSPoint?

    override var isFlipped: Bool { true }

    init(item: Item, width: CGFloat) {
        self.item = item
        super.init(frame: NSRect(x: 0, y: 0, width: width, height: rowHeight))
        item.view = self
        closeButton = symbolButton("xmark", size: 12, target: self, action: #selector(removeItem))
        eyeButton = symbolButton("eye", size: 11, target: self, action: #selector(preview))
        lockButton = symbolButton("lock.open", size: 12, target: self, action: #selector(toggleLock))
        for (i, button) in [closeButton!, eyeButton!, lockButton!].enumerated() {
            button.frame = NSRect(x: width - buttonColumn, y: 8 + CGFloat(i) * 24, width: 22, height: 22)
            addSubview(button)
        }
        closeButton.toolTip = "Remove"
        eyeButton.toolTip = "Quick Look"
        updateButtons()
    }

    required init?(coder: NSCoder) { fatalError() }

    func updateButtons() {
        closeButton.isHidden = !hovering
        eyeButton.isHidden = !hovering
        lockButton.isHidden = !hovering && !item.locked
        let name = item.locked ? "lock.fill" : "lock.open"
        lockButton.image = NSImage(systemSymbolName: name, accessibilityDescription: name)!
            .withSymbolConfiguration(.init(pointSize: 12, weight: .regular))
        lockButton.contentTintColor = item.locked ? .systemYellow : NSColor(white: 1, alpha: 0.8)
        lockButton.toolTip = item.locked ? "Locked: stays after a drop" : "Lock: keep it after a drop"
    }

    var thumbBox: NSRect {
        NSRect(x: (bounds.width - buttonColumn - 56) / 2 + 4, y: 8, width: 56, height: 68)
    }

    // The top file's picture, fitted into the box; a pile's is a little
    // smaller, to leave room for the ones fanned out under it.
    func cardRect(_ i: Int) -> NSRect {
        let box = item.urls.count > 1 ? thumbBox.insetBy(dx: 5, dy: 5) : thumbBox
        let size = item.thumbs[i].size
        guard size.width > 0, size.height > 0 else { return box }
        let k = min(box.width / size.width, box.height / size.height)
        return NSRect(x: box.midX - size.width * k / 2, y: box.midY - size.height * k / 2,
                      width: size.width * k, height: size.height * k)
    }

    override func draw(_ dirtyRect: NSRect) {
        if hovering {
            NSColor(white: 1, alpha: 0.06).setFill()
            NSBezierPath(roundedRect: bounds.insetBy(dx: 6, dy: 2), xRadius: 10, yRadius: 10).fill()
        }
        // Back to front: the third file, the second, the first on top.
        let tilts: [(angle: CGFloat, dx: CGFloat, dy: CGFloat)] = [(0, 0, 0), (-10, -8, 3), (9, 8, 2)]
        for i in (0..<min(item.urls.count, 3)).reversed() {
            let rect = cardRect(i)
            NSGraphicsContext.saveGraphicsState()
            let turn = NSAffineTransform()
            turn.translateX(by: rect.midX + tilts[i].dx, yBy: rect.midY + tilts[i].dy)
            turn.rotate(byDegrees: tilts[i].angle)
            turn.translateX(by: -rect.midX, yBy: -rect.midY)
            turn.concat()
            let shadow = NSShadow()
            shadow.shadowColor = NSColor(white: 0, alpha: 0.45)
            shadow.shadowBlurRadius = 6
            shadow.shadowOffset = NSSize(width: 0, height: -2)
            shadow.set()
            item.thumbs[i].draw(in: rect, from: .zero, operation: .sourceOver, fraction: 1, respectFlipped: true, hints: nil)
            NSGraphicsContext.restoreGraphicsState()
        }
        if item.urls.count > 1 { drawCount() }

        let style = NSMutableParagraphStyle()
        style.alignment = .center
        style.lineBreakMode = .byTruncatingMiddle
        let half = min(thumbBox.midX - 4, bounds.width - 4 - thumbBox.midX)
        item.title.draw(
            in: NSRect(x: thumbBox.midX - half, y: 82, width: half * 2, height: 16),
            withAttributes: [
                .font: NSFont.systemFont(ofSize: 11, weight: .medium),
                .foregroundColor: NSColor(white: 1, alpha: 0.9),
                .paragraphStyle: style,
            ])
    }

    // How many files the pile holds, in a blue badge on its top right corner.
    func drawCount() {
        let text = "\(item.urls.count)" as NSString
        let attrs: [NSAttributedString.Key: Any] = [
            .font: NSFont.systemFont(ofSize: 10, weight: .bold), .foregroundColor: NSColor.white,
        ]
        let size = text.size(withAttributes: attrs)
        let width = max(17, size.width + 8)
        let badge = NSRect(x: thumbBox.maxX - width + 5, y: thumbBox.minY - 3, width: width, height: 17)
        NSColor.systemBlue.setFill()
        NSBezierPath(roundedRect: badge, xRadius: 8.5, yRadius: 8.5).fill()
        text.draw(at: NSPoint(x: badge.midX - size.width / 2, y: badge.midY - size.height / 2), withAttributes: attrs)
    }

    override func updateTrackingAreas() {
        trackingAreas.forEach(removeTrackingArea)
        addTrackingArea(NSTrackingArea(rect: .zero, options: [.mouseEnteredAndExited, .activeAlways, .inVisibleRect],
                                       owner: self, userInfo: nil))
        super.updateTrackingAreas()
    }

    override func mouseEntered(with event: NSEvent) {
        hovering = true
        Shelf.shared.hovered = item
        guard let window else { return }
        let onScreen = window.convertToScreen(convert(thumbBox, to: nil))
        namePill.show(item.names, rightEdge: window.frame.minX, centerY: onScreen.midY)
    }

    override func mouseExited(with event: NSEvent) {
        hovering = false
        if Shelf.shared.hovered === item { Shelf.shared.hovered = nil }
        namePill.hide()
    }

    override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }

    override func mouseDown(with event: NSEvent) {
        mouseDownAt = event.locationInWindow
        guard event.clickCount == 2 else { return }
        if item.urls.count == 1 {
            NSWorkspace.shared.open(item.urls[0])
        } else {
            NSWorkspace.shared.activateFileViewerSelecting(item.urls)
        }
    }

    override func mouseDragged(with event: NSEvent) {
        guard let start = mouseDownAt, hypot(event.locationInWindow.x - start.x, event.locationInWindow.y - start.y) > 3
        else { return }
        mouseDownAt = nil
        namePill.hide()
        // The whole pile goes, its files stacked under the pointer.
        let dragItems = item.urls.enumerated().map { i, url -> NSDraggingItem in
            let dragItem = NSDraggingItem(pasteboardWriter: url as NSURL)
            let image = i < 3 ? item.thumbs[i] : NSWorkspace.shared.icon(forFile: url.path)
            dragItem.setDraggingFrame(cardRect(min(i, 2)), contents: image)
            return dragItem
        }
        let session = beginDraggingSession(with: dragItems, event: event, source: self)
        session.draggingFormation = .pile
    }

    // Copy only: offering move would let Finder move the file away.
    func draggingSession(_ session: NSDraggingSession, sourceOperationMaskFor context: NSDraggingContext) -> NSDragOperation {
        context == .outsideApplication ? .copy : []
    }

    func draggingSession(_ session: NSDraggingSession, endedAt screenPoint: NSPoint, operation: NSDragOperation) {
        if operation != [] && !item.locked { Shelf.shared.remove(item) }
    }

    @objc func removeItem() {
        namePill.hide()
        Shelf.shared.remove(item)
    }

    @objc func preview() { (window as? ShelfPanel)?.togglePreview(item.urls) }

    @objc func toggleLock() {
        item.locked.toggle()
        updateButtons()
    }
}

final class RootView: NSView {
    let scroll = NSScrollView()
    let list = FlippedView()
    let placeholder = NSTextField(labelWithString: "Drop files here")
    var menuButton: NSButton!
    var clearButton: NSButton!
    var dropHighlight = false {
        didSet {
            layer?.borderWidth = dropHighlight ? 2 : 0.5
            updateAlpha()
        }
    }
    var pointerInside = false {
        didSet { updateAlpha() }
    }

    func updateAlpha() {
        NSAnimationContext.runAnimationGroup { context in
            context.duration = 0.15
            window?.animator().alphaValue = pointerInside || dropHighlight ? 1 : restingAlpha
        }
    }

    override func updateTrackingAreas() {
        trackingAreas.forEach(removeTrackingArea)
        addTrackingArea(NSTrackingArea(rect: .zero, options: [.mouseEnteredAndExited, .activeAlways, .inVisibleRect],
                                       owner: self, userInfo: nil))
        super.updateTrackingAreas()
    }

    override func mouseEntered(with event: NSEvent) { pointerInside = true }
    override func mouseExited(with event: NSEvent) { pointerInside = false }

    override init(frame: NSRect) {
        super.init(frame: frame)
        wantsLayer = true
        layer?.cornerRadius = 14
        layer?.masksToBounds = true
        layer?.borderWidth = 0.5
        layer?.borderColor = NSColor(white: 1, alpha: 0.18).cgColor

        let blur = NSVisualEffectView(frame: bounds)
        blur.material = .hudWindow
        blur.blendingMode = .behindWindow
        blur.state = .active
        blur.autoresizingMask = [.width, .height]
        addSubview(blur)

        // A dark layer over the blur, so white names stay readable over a
        // white page behind the shelf.
        let shade = NSView(frame: bounds)
        shade.wantsLayer = true
        shade.layer?.backgroundColor = NSColor(white: 0.08, alpha: 0.45).cgColor
        shade.autoresizingMask = [.width, .height]
        addSubview(shade)

        // A blue glow along the left edge, brightest at the bottom.
        let glow = CAGradientLayer()
        glow.colors = [NSColor.systemBlue.withAlphaComponent(0.35).cgColor, NSColor.systemBlue.withAlphaComponent(0).cgColor]
        glow.startPoint = CGPoint(x: 0, y: 0.2)
        glow.endPoint = CGPoint(x: 0.45, y: 0.5)
        let tint = NSView(frame: bounds)
        tint.layer = glow
        tint.wantsLayer = true
        tint.autoresizingMask = [.width, .height]
        addSubview(tint)

        scroll.frame = NSRect(x: 0, y: footHeight, width: bounds.width, height: bounds.height - footHeight - 4)
        scroll.autoresizingMask = [.width, .height]
        scroll.drawsBackground = false
        scroll.hasVerticalScroller = true
        scroll.autohidesScrollers = true
        scroll.scrollerStyle = .overlay
        scroll.documentView = list
        addSubview(scroll)

        placeholder.font = .systemFont(ofSize: 13, weight: .medium)
        placeholder.textColor = NSColor(white: 1, alpha: 0.5)
        placeholder.alignment = .center
        placeholder.frame = NSRect(x: 0, y: bounds.midY - 10, width: bounds.width, height: 20)
        placeholder.autoresizingMask = [.width, .minYMargin, .maxYMargin]
        addSubview(placeholder)

        menuButton = symbolButton("ellipsis.circle", size: 14, target: self, action: #selector(showMenu))
        menuButton.frame = NSRect(x: 8, y: 6, width: 22, height: 22)
        menuButton.autoresizingMask = [.maxXMargin, .maxYMargin]
        clearButton = symbolButton("delete.left", size: 14, target: self, action: #selector(clearAll))
        clearButton.frame = NSRect(x: bounds.width - 32, y: 6, width: 24, height: 22)
        clearButton.autoresizingMask = [.minXMargin, .maxYMargin]
        clearButton.toolTip = "Clear the shelf"
        addSubview(menuButton)
        addSubview(clearButton)

        registerForDraggedTypes([.fileURL])
    }

    required init?(coder: NSCoder) { fatalError() }

    func reload() {
        list.subviews.forEach { $0.removeFromSuperview() }
        let items = Shelf.shared.items
        for (i, item) in items.enumerated() {
            let view = ItemView(item: item, width: scroll.contentSize.width)
            view.frame.origin.y = 4 + CGFloat(i) * rowHeight
            list.addSubview(view)
        }
        list.frame = NSRect(x: 0, y: 0, width: scroll.contentSize.width,
                            height: max(8 + CGFloat(items.count) * rowHeight, scroll.contentSize.height))
        placeholder.isHidden = !items.isEmpty
        clearButton.isHidden = items.isEmpty
    }

    @objc func clearAll() {
        namePill.hide()
        Shelf.shared.clear()
    }

    @objc func showMenu() {
        let menu = NSMenu()
        let urls = Shelf.shared.items.flatMap(\.urls)
        let add = { (title: String, action: @escaping () -> Void) in
            let entry = ClosureMenuItem(title: title, action: action)
            entry.isEnabled = !urls.isEmpty || title.hasPrefix("Quit")
            menu.addItem(entry)
        }
        add("Reveal in Finder") { NSWorkspace.shared.activateFileViewerSelecting(urls) }
        add("Copy Paths") {
            NSPasteboard.general.clearContents()
            NSPasteboard.general.setString(urls.map(\.path).joined(separator: "\n"), forType: .string)
        }
        menu.addItem(.separator())
        add("Quit Shelf") { NSApp.terminate(nil) }
        menu.autoenablesItems = false
        menu.popUp(positioning: nil, at: NSPoint(x: 0, y: menuButton.bounds.height + 4), in: menuButton)
    }

    private func droppedURLs(_ info: NSDraggingInfo) -> [URL] {
        info.draggingPasteboard.readObjects(forClasses: [NSURL.self], options: [.urlReadingFileURLsOnly: true]) as? [URL] ?? []
    }

    override func draggingEntered(_ sender: NSDraggingInfo) -> NSDragOperation {
        guard !(sender.draggingSource is ItemView), !droppedURLs(sender).isEmpty else { return [] }
        dropHighlight = true
        return .copy
    }

    override func draggingExited(_ sender: NSDraggingInfo?) { dropHighlight = false }

    override func performDragOperation(_ sender: NSDraggingInfo) -> Bool {
        dropHighlight = false
        Shelf.shared.add(droppedURLs(sender).map(\.standardizedFileURL))
        return true
    }
}

final class ClosureMenuItem: NSMenuItem {
    let run: () -> Void

    init(title: String, action: @escaping () -> Void) {
        run = action
        super.init(title: title, action: #selector(fire), keyEquivalent: "")
        target = self
    }

    required init(coder: NSCoder) { fatalError() }

    @objc func fire() { run() }
}

final class ShelfPanel: NSPanel, QLPreviewPanelDataSource {
    var previewURLs: [URL] = []

    override var canBecomeKey: Bool { true }

    override func keyDown(with event: NSEvent) {
        switch event.keyCode {
        case 53: // esc
            if QLPreviewPanel.sharedPreviewPanelExists() && QLPreviewPanel.shared().isVisible {
                QLPreviewPanel.shared().orderOut(nil)
            } else {
                NSApp.terminate(nil)
            }
        case 49: // space
            if let item = Shelf.shared.hovered ?? Shelf.shared.items.first { togglePreview(item.urls) }
        default:
            super.keyDown(with: event)
        }
    }

    // A pile's files are Quick Looked together: the arrow keys go through them.
    func togglePreview(_ urls: [URL]) {
        let panel = QLPreviewPanel.shared()!
        if panel.isVisible && previewURLs == urls {
            panel.orderOut(nil)
            return
        }
        previewURLs = urls
        if panel.isVisible { panel.reloadData() } else { panel.makeKeyAndOrderFront(nil) }
    }

    override func acceptsPreviewPanelControl(_ panel: QLPreviewPanel!) -> Bool { true }
    override func beginPreviewPanelControl(_ panel: QLPreviewPanel!) { panel.dataSource = self }
    override func endPreviewPanelControl(_ panel: QLPreviewPanel!) { panel.dataSource = nil }

    func numberOfPreviewItems(in panel: QLPreviewPanel!) -> Int { previewURLs.count }

    func previewPanel(_ panel: QLPreviewPanel!, previewItemAt index: Int) -> QLPreviewItem! { previewURLs[index] as NSURL }
}

final class AppDelegate: NSObject, NSApplicationDelegate {
    var panel: ShelfPanel!

    var area = NSRect.zero

    // fittedFrame is as tall as the piles (up to 70% of the screen, then they
    // scroll), at the middle of the screen's right edge.
    func fittedFrame() -> NSRect {
        let count = CGFloat(Shelf.shared.items.count)
        let content = count == 0 ? 96 : 8 + count * rowHeight
        let height = min(content + footHeight + 4, area.height * 0.7)
        return NSRect(x: area.maxX - panelWidth - 10, y: area.midY - height / 2, width: panelWidth, height: height)
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        // On the screen the pointer is on.
        let mouse = NSEvent.mouseLocation
        let screen = NSScreen.screens.first { NSMouseInRect(mouse, $0.frame, false) } ?? NSScreen.main!
        area = screen.visibleFrame
        Shelf.shared.add(urls)
        let frame = fittedFrame()

        panel = ShelfPanel(contentRect: frame, styleMask: [.borderless, .nonactivatingPanel], backing: .buffered, defer: false)
        panel.isOpaque = false
        panel.backgroundColor = .clear
        panel.hasShadow = true
        panel.level = .floating
        panel.isMovableByWindowBackground = true
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary]
        panel.appearance = NSAppearance(named: .darkAqua)
        panel.alphaValue = restingAlpha
        let root = RootView(frame: NSRect(origin: .zero, size: frame.size))
        panel.contentView = root
        Shelf.shared.onChange = { [weak self] in
            guard let self else { return }
            self.panel.setFrame(self.fittedFrame(), display: true)
            root.reload()
        }
        root.reload()
        panel.makeKeyAndOrderFront(nil)
        panel.orderFrontRegardless()

        try? "\(getpid())".write(to: pidFile, atomically: true, encoding: .utf8)
        let center = DistributedNotificationCenter.default()
        center.addObserver(forName: addNote, object: nil, queue: .main) { [weak self] note in
            let paths = note.userInfo?["paths"] as? [String] ?? []
            Shelf.shared.add(paths.map { URL(fileURLWithPath: $0).standardizedFileURL })
            self?.panel.orderFrontRegardless()
            center.postNotificationName(ackNote, object: note.object as? String, userInfo: nil, deliverImmediately: true)
        }
    }

    func applicationWillTerminate(_ notification: Notification) {
        if (try? String(contentsOf: pidFile, encoding: .utf8)) == "\(getpid())" {
            try? FileManager.default.removeItem(at: pidFile)
        }
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { false }
}

let addNote = Notification.Name("com.marmota.shelf.add")
let ackNote = Notification.Name("com.marmota.shelf.ack")
let pidFile = URL(fileURLWithPath: NSTemporaryDirectory()).appendingPathComponent("marmota-shelf.pid")

// handedToOpenShelf gives the files to a shelf that is already open, if it
// answers within half a second, so a second Y adds to it instead of opening
// another one.
func handedToOpenShelf(_ paths: [String]) -> Bool {
    guard let text = try? String(contentsOf: pidFile, encoding: .utf8), let pid = Int32(text),
          pid != getpid(), kill(pid, 0) == 0
    else { return false }
    let center = DistributedNotificationCenter.default()
    let token = UUID().uuidString
    var answered = false
    let observer = center.addObserver(forName: ackNote, object: token, queue: nil) { _ in answered = true }
    center.postNotificationName(addNote, object: token, userInfo: ["paths": paths], deliverImmediately: true)
    let deadline = Date().addingTimeInterval(0.5)
    while !answered && Date() < deadline {
        RunLoop.current.run(until: Date().addingTimeInterval(0.02))
    }
    center.removeObserver(observer)
    return answered
}

let urls = CommandLine.arguments.dropFirst().map { URL(fileURLWithPath: $0).standardizedFileURL }
for url in urls where !FileManager.default.fileExists(atPath: url.path) {
    FileHandle.standardError.write("y-drag: no such file: \(url.path)\n".data(using: .utf8)!)
    exit(1)
}
if handedToOpenShelf(urls.map(\.path)) { exit(0) }

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.setActivationPolicy(.accessory) // no Dock icon, and the app you were in stays in front
app.run()
