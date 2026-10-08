// Draws the app icon from the mascot:
// swift scripts/make-icon.swift ios/App/Assets.xcassets/Mascot.imageset/mascot.png ios/App/Assets.xcassets/AppIcon.appiconset/icon.png
import CoreGraphics
import Foundation
import ImageIO
import UniformTypeIdentifiers

let args = CommandLine.arguments
let size = 1024
let space = CGColorSpaceCreateDeviceRGB()
let ctx = CGContext(data: nil, width: size, height: size, bitsPerComponent: 8, bytesPerRow: 0,
                    space: space, bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
func color(_ r: CGFloat, _ g: CGFloat, _ b: CGFloat) -> CGColor { CGColor(colorSpace: space, components: [r, g, b, 1])! }

// Warm cream gradient, lighter at the top.
let gradient = CGGradient(colorsSpace: space, colors: [color(0.99, 0.97, 0.92), color(0.95, 0.90, 0.80)] as CFArray, locations: [0, 1])!
ctx.drawLinearGradient(gradient, start: CGPoint(x: 0, y: 1024), end: .zero, options: [])

// The mascot, large and slightly low, so its ears and face read at small sizes.
let src = CGImageSourceCreateWithURL(URL(fileURLWithPath: args[1]) as CFURL, nil)!
let mascot = CGImageSourceCreateImageAtIndex(src, 0, nil)!
let height: CGFloat = 900
let width = height * CGFloat(mascot.width) / CGFloat(mascot.height)
ctx.draw(mascot, in: CGRect(x: (1024 - width) / 2, y: -40, width: width, height: height))

// Flatten onto an opaque image (App Store icons can't have transparency).
let out = CGContext(data: nil, width: size, height: size, bitsPerComponent: 8, bytesPerRow: 0,
                    space: space, bitmapInfo: CGImageAlphaInfo.noneSkipLast.rawValue)!
out.draw(ctx.makeImage()!, in: CGRect(x: 0, y: 0, width: size, height: size))
let dest = CGImageDestinationCreateWithURL(URL(fileURLWithPath: args[2]) as CFURL, UTType.png.identifier as CFString, 1, nil)!
CGImageDestinationAddImage(dest, out.makeImage()!, nil)
CGImageDestinationFinalize(dest)
