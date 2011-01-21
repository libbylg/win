// Copyright 2011 The Walk Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gui

import (
	"log"
	"os"
	"syscall"
	"unsafe"
)

import (
	"walk/drawing"
)

import (
	. "walk/winapi/comctl32"
	. "walk/winapi/gdi32"
	. "walk/winapi/kernel32"
	. "walk/winapi/user32"
)

const tabWidgetWindowClass = `\o/ Walk_TabWidget_Class \o/`

var tabWidgetWndProcCallback *syscall.Callback

func tabWidgetWndProc(args *uintptr) uintptr {
	msg := msgFromCallbackArgs(args)

	tw, ok := widgetsByHWnd[msg.HWnd].(*TabWidget)
	if !ok {
		// Before CreateWindowEx returns, among others, WM_GETMINMAXINFO is sent.
		// FIXME: Find a way to properly handle this.
		return DefWindowProc(msg.HWnd, msg.Message, msg.WParam, msg.LParam)
	}

	return tw.wndProc(msg, 0)
}

type TabWidget struct {
	Widget
	hWndTab                     HWND
	pages                       *TabPageList
	curPage                     *TabPage
	currentPageChangedPublisher EventPublisher
}

func NewTabWidget(parent IContainer) (*TabWidget, os.Error) {
	if parent == nil {
		return nil, newError("parent cannot be nil")
	}

	ensureRegisteredWindowClass(tabWidgetWindowClass, tabWidgetWndProc, &tabWidgetWndProcCallback)

	hWnd := CreateWindowEx(
		WS_EX_CONTROLPARENT, syscall.StringToUTF16Ptr(tabWidgetWindowClass), nil,
		WS_CHILD|WS_VISIBLE,
		0, 0, 0, 0, parent.Handle(), 0, 0, nil)
	if hWnd == 0 {
		return nil, lastError("CreateWindowEx")
	}

	tw := &TabWidget{
		Widget: Widget{
			hWnd:   hWnd,
			parent: parent,
		},
	}

	succeeded := false
	defer func() {
		if !succeeded {
			tw.Dispose()
		}
	}()

	tw.hWndTab = CreateWindowEx(
		0, syscall.StringToUTF16Ptr("SysTabControl32"), nil,
		WS_CHILD|WS_CLIPSIBLINGS|WS_TABSTOP|WS_VISIBLE,
		0, 0, 0, 0, hWnd, 0, 0, nil)
	if tw.hWndTab == 0 {
		return nil, lastError("CreateWindowEx")
	}
	SendMessage(tw.hWndTab, WM_SETFONT, uintptr(defaultFont.HandleForDPI(0)), 1)

	tw.SetFont(defaultFont)

	if _, err := parent.Children().Add(tw); err != nil {
		return nil, err
	}

	tw.pages = newTabPageList(tw)

	widgetsByHWnd[hWnd] = tw

	succeeded = true

	return tw, nil
}

func (*TabWidget) LayoutFlags() LayoutFlags {
	return GrowHorz | GrowVert | ShrinkHorz | ShrinkVert
}

func (tw *TabWidget) PreferredSize() drawing.Size {
	return tw.dialogBaseUnitsToPixels(drawing.Size{100, 100})
}

func (tw *TabWidget) CurrentPage() *TabPage {
	return tw.curPage
}

func (tw *TabWidget) SetCurrentPage(page *TabPage) os.Error {
	if page == tw.curPage {
		return nil
	}

	index := tw.pages.IndexOf(page)
	if index == -1 {
		return newError("invalid page")
	}

	ret := int(SendMessage(tw.hWndTab, TCM_SETCURSEL, uintptr(index), 0))
	if ret == -1 {
		return newError("SendMessage(TCM_SETCURSEL) failed")
	}

	// FIXME: The SendMessage(TCM_SETCURSEL) call above doesn't cause a
	// TCN_SELCHANGE notification, so we use this workaround.
	tw.onSelChange()

	return nil
}

func (tw *TabWidget) Pages() *TabPageList {
	return tw.pages
}

func (tw *TabWidget) CurrentPageChanged() *Event {
	return tw.currentPageChangedPublisher.Event()
}

func (tw *TabWidget) resizePages() {
	var r RECT
	if !GetWindowRect(tw.hWndTab, &r) {
		log.Println(lastError("GetWindowRect"))
		return
	}

	p := POINT{r.Left, r.Top}
	if !ScreenToClient(tw.hWnd, &p) {
		log.Println(newError("ScreenToClient failed"))
		return
	}

	r = RECT{p.X, p.Y, r.Right - r.Left + p.X, r.Bottom - r.Top + p.Y}

	SendMessage(tw.hWndTab, TCM_ADJUSTRECT, 0, uintptr(unsafe.Pointer(&r)))

	for _, page := range tw.pages.items {
		if err := page.SetBounds(drawing.Rectangle{r.Left - 2, r.Top, r.Right - r.Left + 2, r.Bottom - r.Top}); err != nil {
			log.Println(err)
			return
		}
	}
}

func (tw *TabWidget) onResize(msg *MSG) {
	r := RECT{0, 0, GET_X_LPARAM(msg.LParam), GET_Y_LPARAM(msg.LParam)}
	if !MoveWindow(tw.hWndTab, r.Left, r.Top, r.Right-r.Left, r.Bottom-r.Top, true) {
		log.Println(lastError("MoveWindow"))
		return
	}

	tw.resizePages()
}

func (tw *TabWidget) onSelChange() {
	curIndex := int(SendMessage(tw.hWndTab, TCM_GETCURSEL, 0, 0))

	if tw.curPage != nil {
		if err := tw.curPage.SetVisible(false); err != nil {
			log.Println(err)
			return
		}
	}

	if curIndex == -1 {
		tw.curPage = nil
	} else {
		tw.curPage = tw.pages.At(curIndex)
		if err := tw.curPage.SetVisible(true); err != nil {
			log.Println(err)
			return
		}
		tw.curPage.Invalidate()
	}

	tw.currentPageChangedPublisher.Publish(NewEventArgs(tw))
}

func (tw *TabWidget) wndProc(msg *MSG, origWndProcPtr uintptr) uintptr {
	switch msg.Message {
	case WM_SIZE, WM_SIZING:
		tw.onResize(msg)

	case WM_NOTIFY:
		nmhdr := (*NMHDR)(unsafe.Pointer(msg.LParam))

		switch int(nmhdr.Code) {
		case TCN_SELCHANGE:
			tw.onSelChange()
		}
	}

	return tw.Widget.wndProc(msg, origWndProcPtr)
}

func (tw *TabWidget) onInsertingPage(index int, page *TabPage) (err os.Error) {
	return nil
}

func (tw *TabWidget) onInsertedPage(index int, page *TabPage) (err os.Error) {
	if err = page.SetVisible(false); err != nil {
		return
	}

	style := uint(GetWindowLong(page.hWnd, GWL_STYLE))
	if style == 0 {
		return lastError("GetWindowLong")
	}
	style |= WS_CHILD
	style &^= WS_POPUP
	SetLastError(0)
	if SetWindowLong(page.hWnd, GWL_STYLE, int(style)) == 0 {
		return lastError("SetWindowLong")
	}

	if SetParent(page.hWnd, tw.hWnd) == 0 {
		return lastError("SetParent")
	}

	if tw.pages.Len() == 1 {
		err = page.SetVisible(true)
		if err != nil {
			return
		}
		tw.curPage = page
	}

	text := syscall.StringToUTF16(page.Text())
	item := TCITEM{
		Mask:       TCIF_TEXT,
		PszText:    &text[0],
		CchTextMax: len(text),
	}
	if idx := int(SendMessage(tw.hWndTab, TCM_INSERTITEM, uintptr(index), uintptr(unsafe.Pointer(&item)))); idx == -1 {
		return newError("SendMessage(TCM_INSERTITEM) failed")
	}

	tw.resizePages()

	return
}

func (tw *TabWidget) onRemovingPage(index int, page *TabPage) (err os.Error) {
	panic("not implemented")
}

func (tw *TabWidget) onRemovedPage(index int, page *TabPage) (err os.Error) {
	panic("not implemented")
}

func (tw *TabWidget) onClearingPages() (err os.Error) {
	panic("not implemented")
}

func (tw *TabWidget) onClearedPages() (err os.Error) {
	panic("not implemented")
}
