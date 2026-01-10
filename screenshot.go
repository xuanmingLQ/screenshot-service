package main

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/cdproto/emulation"
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

	// Inject JS to replace the announcement.json URL
	const script = `
	(function() {
		const replacementPrefix = "https://cdn.jsdelivr.net/gh/Exmeaning/Exmeaning-Image-hosting@main";

		function replaceUrl(url) {
			if (typeof url === 'string') {
				// Replace assets.exmeaning.com with the CDN URL, preserving the path.
				// Handles http, https, or no protocol.
				return url.replace(/^(https?:\/\/)?assets\.exmeaning\.com/, replacementPrefix);
			}
			return url;
		}

		const originalFetch = window.fetch;
		window.fetch = function(input, init) {
			if (typeof input === 'string') {
				input = replaceUrl(input);
			} else if (input instanceof Request) {
				// Check if the URL needs replacement
				const newUrl = replaceUrl(input.url);
				if (newUrl !== input.url) {
					input = new Request(newUrl, input);
				}
			}
			return originalFetch(input, init);
		};

		const originalOpen = XMLHttpRequest.prototype.open;
		XMLHttpRequest.prototype.open = function(method, url, ...args) {
			url = replaceUrl(url);
			return originalOpen.call(this, method, url, ...args);
		};
	})();
	`
	tasks = append(tasks, chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument(script).Do(ctx)
		return err
	}))

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
