// Generates the macOS app icon (black squircle + smind logo) used for
// desktop/src-tauri/icons/icon.icns. Usage:
//   swift desktop/scripts/macos-icon.swift desktop/src-tauri/icons/icon.png /tmp/icon-1024.png 660
// then build an .iconset (16..512 @1x/@2x) with sips and `iconutil -c icns`.

import AppKit
let args = CommandLine.arguments
let logoPath = args[1], out = args[2]
let S: CGFloat = 1024
let img = NSImage(size: NSSize(width: S, height: S))
img.lockFocus()
let ctx = NSGraphicsContext.current!
ctx.imageInterpolation = .high
// Apple macOS icon grid: 824x824 body centred, ~185px corner radius.
let body = NSRect(x: 100, y: 100, width: 824, height: 824)
let path = NSBezierPath(roundedRect: body, xRadius: 185, yRadius: 185)
// soft drop shadow like system icons
let sh = NSShadow(); sh.shadowColor = NSColor.black.withAlphaComponent(0.35); sh.shadowBlurRadius = 20; sh.shadowOffset = NSSize(width: 0, height: -8)
NSGraphicsContext.saveGraphicsState(); sh.set(); NSColor.black.setFill(); path.fill(); NSGraphicsContext.restoreGraphicsState()
// near-black vertical gradient for depth
let grad = NSGradient(starting: NSColor(calibratedWhite: 0.13, alpha: 1), ending: NSColor(calibratedWhite: 0.02, alpha: 1))!
grad.draw(in: path, angle: -90)
// hairline inner stroke
NSColor(calibratedWhite: 1, alpha: 0.08).setStroke(); let inner = NSBezierPath(roundedRect: body.insetBy(dx: 1, dy: 1), xRadius: 184, yRadius: 184); inner.lineWidth = 2; inner.stroke()
let logo = NSImage(contentsOfFile: logoPath)!
let L: CGFloat = Double(args[3])!
logo.draw(in: NSRect(x: (S-L)/2, y: (S-L)/2, width: L, height: L), from: .zero, operation: .sourceOver, fraction: 1)
img.unlockFocus()
let rep = NSBitmapImageRep(data: img.tiffRepresentation!)!
try! rep.representation(using: .png, properties: [:])!.write(to: URL(fileURLWithPath: out))
