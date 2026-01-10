package main

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

func TakeScreenshot(req *ScreenshotRequest) ([]byte, string, error) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("disable-background-networking", true),
		chromedp.Flag("disable-sync", true),
		chromedp.Flag("disable-translate", true),
		chromedp.Flag("hide-scrollbars", true),
		chromedp.Flag("metrics-recording-only", true),
		chromedp.Flag("mute-audio", true),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("safebrowsing-disable-auto-update", true),
		chromedp.WindowSize(req.Width, req.Height),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer allocCancel()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	ctx, cancel = context.WithTimeout(ctx, time.Duration(req.Timeout)*time.Second)
	defer cancel()

	var tasks []chromedp.Action

	if len(req.Headers) > 0 || req.UserAgent != "" {
		headers := make(map[string]interface{})
		for k, v := range req.Headers {
			headers[k] = v
		}
		if req.UserAgent != "" {
			headers["User-Agent"] = req.UserAgent
		}
		tasks = append(tasks, network.Enable(), network.SetExtraHTTPHeaders(headers))
	}

	tasks = append(tasks, chromedp.ActionFunc(func(ctx context.Context) error {
		orientation := emulation.OrientationTypePortraitPrimary
		if req.Landscape {
			orientation = emulation.OrientationTypeLandscapePrimary
		}

		return emulation.SetDeviceMetricsOverride(
			int64(req.Width),
			int64(req.Height),
			req.DeviceScale,
			req.Mobile,
		).WithScreenOrientation(&emulation.ScreenOrientation{
			Type:  orientation,
			Angle: 0,
		}).Do(ctx)
	}))

	// Enable Fetch domain to intercept requests
	tasks = append(tasks, fetch.Enable())

	// Listen for request handling
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		if ev, ok := ev.(*fetch.EventRequestPaused); ok {
			go func() {
				reqURL := ev.Request.URL
				// Check if the URL starts with the target domain (http or https or no protocol)
				// We need a simple string check or regex. Since we want to replace the host but keep the path.
				// Regex to match assets.exmeaning.com and optional protocol
				// Using simple string replacement for simplicity if possible, but regex is safer for domain matching.

				// Logic:
				// If URL matches `assets.exmeaning.com`, replace the domain with `exmeaning-image-hosting.zeabur.internal:8080`
				// and keep the rest of the path.

				// Let's use string manipulation for efficiency if it's consistent.
				// Or just regex as before.

				// Note: Go's regexp package is not available here unless imported, but we are inside a function.
				// We can just use strings.HasPrefix or Contains.
				// However, to do it robustly like the valid regexp replacement:

				// We will iterate to check if it needs replacement.

				// Simple approach:
				// If strings.Contains(reqURL, "assets.exmeaning.com") {
				//    newURL := strings.Replace(reqURL, "https://assets.exmeaning.com", "http://exmeaning-image-hosting.zeabur.internal:8080", 1)
				//    newURL = strings.Replace(newURL, "http://assets.exmeaning.com", "http://exmeaning-image-hosting.zeabur.internal:8080", 1)
				//    // Handle case without protocol if it happens (unlikely in standardized RequestURL)
				//    cc.Executor.Execute(context.Background(), fetch.ContinueRequest(ev.RequestId).WithUrl(newURL))
				// } else {
				//    cc.Executor.Execute(context.Background(), fetch.ContinueRequest(ev.RequestId))
				// }

				// Wait, `chromedp.ListenTarget` callback is executed synchronously.
				// We shouldn't use `go func()` unless strictly necessary, but `Executor.Execute` might block?
				// Actually `chromedp` documentation says listeners block the event loop? No, they are callbacks.
				// But `fetch.ContinueRequest` needs to be sent.

				// Better approach inside `chromdp` is often just to invoke the command.

				targetDomain := "assets.exmeaning.com"
				replacementBase := "http://exmeaning-image-hosting.zeabur.internal:8080"

				needsReplacement := false
				newURL := reqURL

				// Handle specific cases
				if len(reqURL) > 8 && reqURL[0:8] == "https://" && len(reqURL) >= 8+len(targetDomain) && reqURL[8:8+len(targetDomain)] == targetDomain {
					newURL = replacementBase + reqURL[8+len(targetDomain):]
					needsReplacement = true
				} else if len(reqURL) > 7 && reqURL[0:7] == "http://" && len(reqURL) >= 7+len(targetDomain) && reqURL[7:7+len(targetDomain)] == targetDomain {
					newURL = replacementBase + reqURL[7+len(targetDomain):]
					needsReplacement = true
				}

				// Execute ContinueRequest
				if needsReplacement {
					// fmt.Printf("Replacing URL: %s -> %s\n", reqURL, newURL)
					err := fetch.ContinueRequest(ev.RequestID).WithURL(newURL).Do(ctx)
					if err != nil {
						fmt.Printf("Failed to continue request with modified URL: %v\n", err)
					}
				} else {
					err := fetch.ContinueRequest(ev.RequestID).Do(ctx)
					if err != nil {
						fmt.Printf("Failed to continue request: %v\n", err)
					}
				}
			}()
		}
	})

	tasks = append(tasks, chromedp.Navigate(req.URL))

	if req.WaitFor != "" {
		tasks = append(tasks, chromedp.WaitVisible(req.WaitFor, chromedp.ByQuery))
	}

	if req.WaitTime > 0 {
		tasks = append(tasks, chromedp.Sleep(time.Duration(req.WaitTime)*time.Millisecond))
	}

	var buf []byte

	if req.FullPage {
		tasks = append(tasks, fullPageScreenshot(&buf, req.Format, req.Quality))
	} else if req.Clip != nil {
		tasks = append(tasks, clipScreenshot(&buf, req))
	} else {
		tasks = append(tasks, viewportScreenshot(&buf, req.Format, req.Quality))
	}

	if err := chromedp.Run(ctx, tasks...); err != nil {
		return nil, "", fmt.Errorf("screenshot failed: %w", err)
	}

	contentType := getContentType(req.Format)
	return buf, contentType, nil
}

func getChromeFormat(format string) page.CaptureScreenshotFormat {
	switch format {
	case "jpeg", "jpg":
		return page.CaptureScreenshotFormatJpeg
	case "webp":
		return page.CaptureScreenshotFormatWebp
	default:
		return page.CaptureScreenshotFormatPng
	}
}

func getContentType(format string) string {
	switch format {
	case "jpeg", "jpg":
		return "image/jpeg"
	case "webp":
		return "image/webp"
	default:
		return "image/png"
	}
}

func viewportScreenshot(buf *[]byte, format string, quality int) chromedp.ActionFunc {
	return func(ctx context.Context) error {
		chromeFormat := getChromeFormat(format)

		capture := page.CaptureScreenshot().WithFormat(chromeFormat)

		if chromeFormat == page.CaptureScreenshotFormatJpeg ||
			chromeFormat == page.CaptureScreenshotFormatWebp {
			capture = capture.WithQuality(int64(quality))
		}

		var err error
		*buf, err = capture.Do(ctx)
		return err
	}
}

func fullPageScreenshot(buf *[]byte, format string, quality int) chromedp.ActionFunc {
	return func(ctx context.Context) error {
		_, _, contentSize, _, _, _, err := page.GetLayoutMetrics().Do(ctx)
		if err != nil {
			return err
		}

		width, height := int64(contentSize.Width), int64(contentSize.Height)

		if height > 16384 {
			height = 16384
		}

		if err := emulation.SetDeviceMetricsOverride(width, height, 1, false).Do(ctx); err != nil {
			return err
		}

		chromeFormat := getChromeFormat(format)

		capture := page.CaptureScreenshot().
			WithFormat(chromeFormat).
			WithCaptureBeyondViewport(true)

		if chromeFormat == page.CaptureScreenshotFormatJpeg ||
			chromeFormat == page.CaptureScreenshotFormatWebp {
			capture = capture.WithQuality(int64(quality))
		}

		*buf, err = capture.Do(ctx)
		return err
	}
}

func clipScreenshot(buf *[]byte, req *ScreenshotRequest) chromedp.ActionFunc {
	return func(ctx context.Context) error {
		chromeFormat := getChromeFormat(req.Format)

		capture := page.CaptureScreenshot().
			WithFormat(chromeFormat).
			WithClip(&page.Viewport{
				X:      req.Clip.X,
				Y:      req.Clip.Y,
				Width:  req.Clip.Width,
				Height: req.Clip.Height,
				Scale:  1,
			})

		if chromeFormat == page.CaptureScreenshotFormatJpeg ||
			chromeFormat == page.CaptureScreenshotFormatWebp {
			capture = capture.WithQuality(int64(req.Quality))
		}

		var err error
		*buf, err = capture.Do(ctx)
		return err
	}
}
