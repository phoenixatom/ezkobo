// Draws the app icon: swift scripts/make-icon.swift ios/App/Assets.xcassets/AppIcon.appiconset/icon.png
import CoreGraphics
import Foundation
import ImageIO
import UniformTypeIdentifiers

let size = 1024
let space = CGColorSpaceCreateDeviceRGB()
let ctx = CGContext(data: nil, width: size, height: size, bitsPerComponent: 8, bytesPerRow: 0,
                    space: space, bitmapInfo: CGImageAlphaInfo.noneSkipLast.rawValue)!

func color(_ r: CGFloat, _ g: CGFloat, _ b: CGFloat, _ a: CGFloat = 1) -> CGColor {
    CGColor(colorSpace: space, components: [r, g, b, a])!
}
func rounded(_ rect: CGRect, _ radius: CGFloat) -> CGPath {
    CGPath(roundedRect: rect, cornerWidth: radius, cornerHeight: radius, transform: nil)
}

// Background: deep green with a subtle vertical gradient.
let gradient = CGGradient(colorsSpace: space, colors: [color(0.20, 0.42, 0.36), color(0.12, 0.29, 0.25)] as CFArray,
                          locations: [0, 1])!
ctx.drawLinearGradient(gradient, start: CGPoint(x: 0, y: 1024), end: .zero, options: [])

// E-reader body outline.
ctx.addPath(rounded(CGRect(x: 292, y: 212, width: 440, height: 600), 64))
ctx.setStrokeColor(color(1, 1, 1))
ctx.setLineWidth(36)
ctx.strokePath()

// Screen.
ctx.addPath(rounded(CGRect(x: 342, y: 266, width: 288, height: 492), 14))
ctx.setFillColor(color(1, 1, 1, 0.94))
ctx.fillPath()

// Page-turn buttons.
ctx.setFillColor(color(1, 1, 1))
for y in [462.0, 538.0] {
    ctx.addPath(rounded(CGRect(x: 664, y: y, width: 18, height: 52), 9))
}
ctx.fillPath()

// Lines of text.
ctx.setFillColor(color(0.16, 0.35, 0.30, 0.5))
for (i, w) in [208.0, 230, 190, 222, 160].enumerated() {
    ctx.addPath(rounded(CGRect(x: 382, y: 680 - Double(i) * 62, width: w, height: 22), 11))
}
ctx.fillPath()

let url = URL(fileURLWithPath: CommandLine.arguments[1]) as CFURL
let dest = CGImageDestinationCreateWithURL(url, UTType.png.identifier as CFString, 1, nil)!
CGImageDestinationAddImage(dest, ctx.makeImage()!, nil)
CGImageDestinationFinalize(dest)
