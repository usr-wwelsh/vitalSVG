package badge

import (
	"embed"
	"fmt"
	"io"
	"math"
	"strings"
	"text/template"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

var templates *template.Template

func init() {
	templates = template.Must(template.ParseFS(templateFS, "templates/*.tmpl"))
}

// StatusData holds the template data for a status/metric badge.
type StatusData struct {
	Label      string
	Value      string
	Color      string
	LabelWidth int
	ValueWidth int
	TotalWidth int
	LabelX     int // center of label section, scaled 10x for SVG
	ValueX     int // center of value section, scaled 10x for SVG
}

// SparklineData holds the template data for a sparkline badge.
type SparklineData struct {
	Label      string
	Color      string
	Width      int
	Height     int
	Points     string // SVG polyline points for the line
	AreaPoints string // SVG polyline points for the filled area
	MidX       int
	MidY       int
}

// DataPoint is a single value in a time series.
type DataPoint struct {
	Value float64
	Time  int64 // unix timestamp
}

// Color constants matching shields.io palette.
const (
	ColorGreen  = "#4c1"
	ColorYellow = "#dfb317"
	ColorRed    = "#e05d44"
	ColorGray   = "#9f9f9f"
)

// estimateTextWidth approximates the pixel width of text rendered in Verdana 11px.
func estimateTextWidth(s string) int {
	w := 0.0
	for _, c := range s {
		switch {
		case c >= 'A' && c <= 'Z':
			w += 7.5
		case c >= '0' && c <= '9':
			w += 6.5
		case c == ' ':
			w += 3.0
		case c == '.' || c == ':':
			w += 3.5
		case c == '%':
			w += 8.0
		default:
			w += 6.1
		}
	}
	return int(math.Ceil(w))
}

func newStatusData(label, value, color string) StatusData {
	lw := estimateTextWidth(label) + 10 // padding
	vw := estimateTextWidth(value) + 10
	return StatusData{
		Label:      label,
		Value:      value,
		Color:      color,
		LabelWidth: lw,
		ValueWidth: vw,
		TotalWidth: lw + vw,
		LabelX:     lw * 5,        // center of label, scaled 10x
		ValueX:     lw*10 + vw*5,  // center of value section, scaled 10x
	}
}

// StatusColor returns a color based on the status string.
func StatusColor(status string) string {
	switch strings.ToLower(status) {
	case "online", "running", "healthy":
		return ColorGreen
	case "degraded", "unhealthy", "paused":
		return ColorYellow
	case "offline", "stopped", "exited", "dead":
		return ColorRed
	default:
		return ColorGray
	}
}

// MetricColor returns a color based on a percentage value (0-100).
func MetricColor(pct float64) string {
	switch {
	case pct >= 85:
		return ColorRed
	case pct >= 60:
		return ColorYellow
	default:
		return ColorGreen
	}
}

// RenderStatus writes a status badge SVG to w.
func RenderStatus(w io.Writer, label, status string) error {
	data := newStatusData(label, status, StatusColor(status))
	return templates.ExecuteTemplate(w, "status.svg.tmpl", data)
}

// RenderMetric writes a metric badge SVG to w.
func RenderMetric(w io.Writer, label string, value float64, unit string) error {
	var display string
	var color string

	switch unit {
	case "%":
		display = fmt.Sprintf("%.1f%%", value)
		color = MetricColor(value)
	case "uptime":
		display = formatUptime(value)
		color = ColorGreen
	default:
		display = fmt.Sprintf("%.1f %s", value, unit)
		color = ColorGreen
	}

	data := newStatusData(label, display, color)
	return templates.ExecuteTemplate(w, "metric.svg.tmpl", data)
}

// RenderSparkline writes a sparkline badge SVG to w.
func RenderSparkline(w io.Writer, label string, points []DataPoint) error {
	width := 120
	height := 30
	chartTop := 14.0    // leave room for label text
	chartBottom := 28.0 // small bottom padding

	data := SparklineData{
		Label:  label,
		Color:  ColorGreen,
		Width:  width,
		Height: height,
		MidX:   width / 2,
		MidY:   height/2 + 4,
	}

	if len(points) >= 2 {
		// Find min/max for normalization
		minVal, maxVal := points[0].Value, points[0].Value
		for _, p := range points[1:] {
			if p.Value < minVal {
				minVal = p.Value
			}
			if p.Value > maxVal {
				maxVal = p.Value
			}
		}

		// Determine color from latest value
		latest := points[len(points)-1].Value
		if maxVal <= 100 { // assume percentage
			data.Color = MetricColor(latest)
		}

		valRange := maxVal - minVal
		if valRange == 0 {
			valRange = 1 // avoid division by zero, draws flat line at midpoint
		}

		chartLeft := 4.0
		chartRight := float64(width) - 4.0
		chartWidth := chartRight - chartLeft
		chartHeight := chartBottom - chartTop

		var linePts, areaPts []string
		for i, p := range points {
			x := chartLeft + (float64(i)/float64(len(points)-1))*chartWidth
			y := chartBottom - ((p.Value-minVal)/valRange)*chartHeight
			pt := fmt.Sprintf("%.1f,%.1f", x, y)
			linePts = append(linePts, pt)
			areaPts = append(areaPts, pt)
		}

		// Close the area polygon along the bottom
		areaPts = append(areaPts, fmt.Sprintf("%.1f,%.1f", chartRight, chartBottom))
		areaPts = append(areaPts, fmt.Sprintf("%.1f,%.1f", chartLeft, chartBottom))

		data.Points = strings.Join(linePts, " ")
		data.AreaPoints = strings.Join(areaPts, " ")
	}

	return templates.ExecuteTemplate(w, "sparkline.svg.tmpl", data)
}

// RenderUnknown writes a gray "unknown" badge SVG to w.
func RenderUnknown(w io.Writer, label string) error {
	data := newStatusData(label, "unknown", ColorGray)
	return templates.ExecuteTemplate(w, "status.svg.tmpl", data)
}

// RenderRAM writes a RAM badge showing percentage and human-readable usage.
func RenderRAM(w io.Writer, label string, pct, used, limit float64) error {
	display := fmt.Sprintf("%.0f%% (%s)", pct, formatBytes(used))
	color := MetricColor(pct)
	data := newStatusData(label, display, color)
	return templates.ExecuteTemplate(w, "metric.svg.tmpl", data)
}

func formatBytes(b float64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1fG", b/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.0fM", b/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.0fK", b/(1<<10))
	default:
		return fmt.Sprintf("%.0fB", b)
	}
}

func formatUptime(seconds float64) string {
	s := int(seconds)
	days := s / 86400
	hours := (s % 86400) / 3600
	mins := (s % 3600) / 60

	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}
