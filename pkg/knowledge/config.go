package knowledge

import (
	"time"

	"github.com/andrewneudegg/lab/pkg/config"
)

func TextExtractionOptionsFromConfig(cfg config.KnowledgeConfig) TextExtractionOptions {
	ocrEnabled := true
	if cfg.OCR.Enabled != nil {
		ocrEnabled = *cfg.OCR.Enabled
	}
	return TextExtractionOptions{
		PDFTextCommand: cfg.PDFTextCommand,
		PDFOCR: PDFOCROptions{
			Disabled:         !ocrEnabled,
			PDFToPPMCommand:  cfg.OCR.PDFToPPMCommand,
			TesseractCommand: cfg.OCR.TesseractCommand,
			Language:         cfg.OCR.Language,
			DPI:              cfg.OCR.DPI,
			MaxPages:         cfg.OCR.MaxPages,
			Timeout:          time.Duration(cfg.OCR.TimeoutSeconds) * time.Second,
		},
	}
}
