//go:build darwin && cgo

#import <AppKit/AppKit.h>
#include <stdint.h>

// Native input views bypass renderer hit testing and accept the first click
// while the overlay is inactive. Keep the close button outside the drag region.
@interface VTCaptionDragHandle : NSView
@end

@implementation VTCaptionDragHandle
- (BOOL)isOpaque { return NO; }
- (BOOL)acceptsFirstMouse:(NSEvent *)event { return YES; }
- (BOOL)mouseDownCanMoveWindow { return NO; }
- (void)resetCursorRects {
    [self addCursorRect:self.bounds cursor:[NSCursor openHandCursor]];
}
- (void)mouseDown:(NSEvent *)event {
    [self.window performWindowDragWithEvent:event];
}
@end

// A native grip tracks pointer dragging without fighting the UI renderer.
@interface VTCaptionResizeGrip : NSView
@end

@implementation VTCaptionResizeGrip
- (BOOL)isOpaque { return NO; }
- (BOOL)acceptsFirstMouse:(NSEvent *)event { return YES; }
- (BOOL)mouseDownCanMoveWindow { return NO; }
- (void)resetCursorRects {
    [self addCursorRect:self.bounds cursor:[NSCursor crosshairCursor]];
}
- (void)drawRect:(NSRect)dirtyRect {
    [[NSColor colorWithWhite:1 alpha:0.5] setStroke];
    for (int offset = 5; offset <= 15; offset += 5) {
        NSBezierPath *line = [NSBezierPath bezierPath];
        [line moveToPoint:NSMakePoint(self.bounds.size.width - offset - 3, 4)];
        [line lineToPoint:NSMakePoint(self.bounds.size.width - 4, offset + 3)];
        line.lineWidth = 1.5;
        [line stroke];
    }
}
- (void)mouseDown:(NSEvent *)event {
    NSWindow *window = self.window;
    NSRect initial = window.frame;
    NSPoint start = [NSEvent mouseLocation];
    while (YES) {
        NSEvent *next = [window nextEventMatchingMask:NSEventMaskLeftMouseDragged | NSEventMaskLeftMouseUp];
        if (!next || next.type == NSEventTypeLeftMouseUp) { break; }
        NSPoint current = [NSEvent mouseLocation];
        CGFloat width = MAX(window.minSize.width, initial.size.width + current.x - start.x);
        CGFloat height = MAX(window.minSize.height, initial.size.height + start.y - current.y);
        // Anchor the top-left; Cocoa screen coordinates start at bottom-left.
        NSRect frame = NSMakeRect(initial.origin.x, NSMaxY(initial) - height, width, height);
        [window setFrame:frame display:YES];
    }
}
@end

void vt_configure_caption_overlay(uintptr_t handle) {
    if (handle == 0) {
        return;
    }
    NSWindow *window = (__bridge NSWindow *)(void *)handle;
    // Preserve MyGo's other collection flags; this remains an ordinary window,
    // not a fullscreen primary window or a replacement NSWindow subclass.
    window.collectionBehavior |= NSWindowCollectionBehaviorCanJoinAllSpaces |
                                 NSWindowCollectionBehaviorFullScreenAuxiliary;
    window.level = NSFloatingWindowLevel;
    window.movableByWindowBackground = NO;
    window.hidesOnDeactivate = NO;
    window.styleMask |= NSWindowStyleMaskResizable;
    NSView *content = window.contentView;
    BOOL flipped = content.isFlipped;
    CGFloat headerY = flipped ? 26 : NSHeight(content.bounds) - 70;
    VTCaptionDragHandle *drag = [[VTCaptionDragHandle alloc]
        initWithFrame:NSMakeRect(26, headerY, MAX(0, NSWidth(content.bounds) - 190), 44)];
    drag.autoresizingMask = NSViewWidthSizable | (flipped ? NSViewMaxYMargin : NSViewMinYMargin);
    [content addSubview:drag positioned:NSWindowAbove relativeTo:nil];
    VTCaptionResizeGrip *grip = [[VTCaptionResizeGrip alloc]
        initWithFrame:NSMakeRect(NSWidth(content.bounds) - 30,
            flipped ? NSHeight(content.bounds) - 30 : 8, 22, 22)];
    grip.autoresizingMask = NSViewMinXMargin | (flipped ? NSViewMinYMargin : NSViewMaxYMargin);
    [content addSubview:grip positioned:NSWindowAbove relativeTo:nil];
#if !__has_feature(objc_arc)
    [drag release];
    [grip release];
#endif
    // Showing uses MyGo's ShowInactive, rather than making this window key or
    // activating the app. Fullscreen/exclusive presentation can still obscure it.
}
