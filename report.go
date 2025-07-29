package main

import (
	"encoding/csv"
	"fmt"
	"log/slog"
	"os"
	"strconv"
)

func generateReports(config *Config, results []BenchmarkResult, domains []string) error {
	// Generate main report
	if config.GeneralReportPath != "" {
		if err := writeMainReport(config.GeneralReportPath, results); err != nil {
			return fmt.Errorf("writing main report: %w", err)
		}
	}

	// Generate matrix report
	if config.MatrixReportPath != "" {
		if err := writeMatrixReport(config.MatrixReportPath, results, domains); err != nil {
			return fmt.Errorf("writing matrix report: %w", err)
		}
	}

	return nil
}

func writeMainReport(path string, results []BenchmarkResult) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	header := []string{
		"Name", "Address", "Min(ms)", "Max(ms)", "Mean(ms)", "Median(ms)",
		"Successful", "Errors", "Total", "Success Rate(%)",
	}

	if err := writer.Write(header); err != nil {
		return err
	}

	for _, result := range results {
		record := []string{
			result.Server.Name,
			result.Server.Addr,
			formatFloat(result.Stats.Min),
			formatFloat(result.Stats.Max),
			formatFloat(result.Stats.Mean),
			formatFloat(result.Stats.Median),
			strconv.Itoa(result.Stats.Count),
			strconv.Itoa(result.Stats.Errors),
			strconv.Itoa(result.Stats.Total),
			fmt.Sprintf("%.1f", result.Stats.SuccessRate()*100),
		}

		if err := writer.Write(record); err != nil {
			return err
		}
	}

	slog.Info("Main report written", slog.String("path", path))
	return nil
}

func writeMatrixReport(path string, results []BenchmarkResult, domains []string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Build header
	header := []string{"Domain"}
	for _, result := range results {
		header = append(header, result.Server.Name)
	}

	if err := writer.Write(header); err != nil {
		return err
	}

	// Write data rows
	for _, domain := range domains {
		row := []string{domain}

		for _, result := range results {
			if mean, exists := result.DomainMean[domain]; exists {
				row = append(row, formatFloat(mean))
			} else {
				row = append(row, "")
			}
		}

		if err := writer.Write(row); err != nil {
			return err
		}
	}

	slog.Info("Matrix report written", slog.String("path", path))
	return nil
}
