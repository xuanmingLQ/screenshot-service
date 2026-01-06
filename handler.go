package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type ScreenshotRequest struct {
	URL         string            `json:"url" form:"url" binding:"required"`
	Width       int               `json:"width" form:"width"`
	Height      int               `json:"height" form:"height"`
	Format      string            `json:"format" form:"format"`
	Quality     int               `json:"quality" form:"quality"`
	WaitTime    int               `json:"wait_time" form:"wait_time"`
	WaitFor     string            `json:"wait_for" form:"wait_for"`
	FullPage    bool              `json:"full_page" form:"full_page"`
	Headers     map[string]string `json:"headers" form:"-"`
	HeadersRaw  string            `json:"-" form:"headers"`
	UserAgent   string            `json:"user_agent" form:"user_agent"`
	Clip        *ClipRect         `json:"clip" form:"-"`
	ClipRaw     string            `json:"-" form:"clip"`
	DeviceScale float64           `json:"device_scale" form:"device_scale"`
	Mobile      bool              `json:"mobile" form:"mobile"`
	Landscape   bool              `json:"landscape" form:"landscape"`
	Timeout     int               `json:"timeout" form:"timeout"`
}

type ClipRect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

func (r *ScreenshotRequest) SetDefaults() {
	if r.Width <= 0 {
		r.Width = 1920
	}
	if r.Height <= 0 {
		r.Height = 1080
	}
	if r.Format == "" {
		r.Format = "png"
	}
	r.Format = strings.ToLower(r.Format)
	if r.Quality <= 0 || r.Quality > 100 {
		r.Quality = 90
	}
	if r.WaitTime < 0 {
		r.WaitTime = 0
	}
	if r.DeviceScale <= 0 {
		r.DeviceScale = 1.0
	}
	if r.Timeout <= 0 {
		r.Timeout = 30
	}
	if r.Timeout > 120 {
		r.Timeout = 120
	}
}

func (r *ScreenshotRequest) Validate() error {
	validFormats := map[string]bool{"png": true, "jpeg": true, "jpg": true, "webp": true}
	if !validFormats[r.Format] {
		return &ValidationError{Field: "format", Message: "must be png, jpeg, or webp"}
	}
	if r.Width > 4096 || r.Width < 100 {
		return &ValidationError{Field: "width", Message: "must be between 100 and 4096"}
	}
	if r.Height > 10000 || r.Height < 100 {
		return &ValidationError{Field: "height", Message: "must be between 100 and 10000"}
	}
	return nil
}

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}

func HandleScreenshotGet(c *gin.Context) {
	var req ScreenshotRequest

	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.HeadersRaw != "" {
		if err := json.Unmarshal([]byte(req.HeadersRaw), &req.Headers); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid headers JSON"})
			return
		}
	}

	if req.ClipRaw != "" {
		var clip ClipRect
		if err := json.Unmarshal([]byte(req.ClipRaw), &clip); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid clip JSON"})
			return
		}
		req.Clip = &clip
	}

	processScreenshot(c, &req)
}

func HandleScreenshotPost(c *gin.Context) {
	var req ScreenshotRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	processScreenshot(c, &req)
}

func processScreenshot(c *gin.Context, req *ScreenshotRequest) {
	req.SetDefaults()

	if err := req.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	data, contentType, err := TakeScreenshot(req)
	if err != nil {
		log.Printf("Screenshot failed for URL: %s, Error: %v, Req: %+v", req.URL, err, req)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Header("Content-Type", contentType)
	c.Header("Content-Length", strconv.Itoa(len(data)))
	c.Header("Cache-Control", "public, max-age=86400")

	filename := "screenshot." + req.Format
	if req.Format == "jpg" {
		filename = "screenshot.jpeg"
	}
	c.Header("Content-Disposition", "inline; filename=\""+filename+"\"")

	c.Data(http.StatusOK, contentType, data)
}
