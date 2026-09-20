package read

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"

	"crdx.org/oh/internal/file"

	"crdx.org/oh/internal/util"
	"crdx.org/oh/internal/util/imageutil"
	"crdx.org/oh/internal/util/strutil"
	"crdx.org/oh/pkg/tool"
)

const (
	maxFileBytes  = 20 * 1024 * 1024
	bytePrecision = 3
)

var errFileTooLarge = errors.New("file is too large to read")

type loadedFile struct {
	data      []byte
	mediaType string
	size      int64
}

type loadedRange struct {
	contentHash   string
	output        string
	selectedLines int64
	totalLines    int64
}

type Args struct {
	Path   string `json:"path"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

func New(root *file.Root, snapshots *file.Snapshots) tool.Tool {
	restoreReadState := func(payload json.RawMessage) error {
		return snapshots.RestoreReadState(root, payload)
	}

	return tool.Implement(
		tool.Definition{
			Name:        "read",
			Description: "read a text or image file",
			Schema: tool.Schema{
				tool.String("path", "file"),
				tool.Integer("offset", "first line, 1-indexed").Optional(),
				tool.Integer("limit", "max lines").Optional(),
			},
		},
		Describe,
	).
		State(file.FileReadState, restoreReadState).
		FocusPath().
		IsEmbarrassinglyParallel().
		ChangesNothing().
		Run(func(ctx context.Context, args Args) (tool.ToolCallResult, error) {
			return exec(ctx, root, args)
		})
}

func Describe(args Args) (string, string) {
	return args.Path, span(args.Offset, args.Limit)
}

func span(offset int, limit int) string {
	switch {
	case offset > 0 && limit > 0:
		return fmt.Sprintf("%d-%d", offset, offset+limit-1)
	case offset > 0:
		return fmt.Sprintf("%d+", offset)
	case limit > 0:
		return fmt.Sprintf("1-%d", limit)
	default:
		return ""
	}
}

func exec(ctx context.Context, root *file.Root, args Args) (tool.ToolCallResult, error) {
	if args.Path == "" {
		return tool.ToolCallResult{}, errors.New("path is required")
	}

	root, name, err := root.Resolve(args.Path)
	if err != nil {
		return tool.ToolCallResult{}, err
	}

	loadedFile, err := load(root, name)
	metrics := tool.ToolCallMetrics{Kind: tool.MetricRead, Bytes: loadedFile.size}
	if errors.Is(err, errFileTooLarge) {
		return oversizedResult(ctx, root, name, args, loadedFile.mediaType, metrics)
	}
	if err != nil {
		return tool.ToolCallResult{}, readFailure(args.Path, err)
	}

	data := loadedFile.data
	mediaType := loadedFile.mediaType
	if imageutil.IsSupported(mediaType) {
		if args.Offset > 0 || args.Limit > 0 {
			return tool.ToolCallResult{Metrics: metrics}, errors.New("images do not support line ranges")
		}

		metrics.Kind = tool.MetricImage
		if width, height, ok := imageutil.Dimensions(data); ok {
			metrics.EstimatedTokens = util.EstimateImageTokenCount(imageutil.Fit(width, height))
		}

		return successfulResult(
			args.Path,
			data,
			fmt.Sprintf("%s, %s", mediaType, util.FormatBytes(len(data), bytePrecision)),
			tool.Image{MediaType: mediaType, Data: data},
			metrics,
		), nil
	}

	lines := strutil.Lines(string(data))
	metrics.Lines = int64(len(lines))

	if args.Offset <= 0 && args.Limit <= 0 {
		return successfulResult(args.Path, data, string(data), tool.Image{}, metrics), nil
	}

	start := 0
	if args.Offset > 0 {
		start = args.Offset - 1
	}
	if len(lines) == 0 {
		return successfulResult(args.Path, data, "", tool.Image{}, metrics), nil
	}
	if start >= len(lines) {
		return tool.ToolCallResult{Metrics: metrics}, fmt.Errorf(
			"offset %d exceeds the file's %d lines", args.Offset, len(lines),
		)
	}

	end := len(lines)
	if args.Limit > 0 && start+args.Limit < end {
		end = start + args.Limit
	}

	output := strings.Join(lines[start:end], "\n")
	metrics.Lines = int64(end - start)
	metrics.Bytes = int64(len(output))
	return successfulResult(args.Path, data, output, tool.Image{}, metrics), nil
}

func oversizedResult(
	ctx context.Context,
	root *file.Root,
	name string,
	args Args,
	mediaType string,
	metrics tool.ToolCallMetrics,
) (tool.ToolCallResult, error) {
	isRange := args.Offset > 0 || args.Limit > 0
	isImage := imageutil.IsSupported(mediaType)
	if !isRange {
		noun := "file"
		if isImage {
			noun = "image"
		}
		return tool.ToolCallResult{Metrics: metrics}, fmt.Errorf("%s exceeds the %s limit", noun, util.FormatBytes(maxFileBytes, bytePrecision))
	}
	if isImage {
		return tool.ToolCallResult{Metrics: metrics}, errors.New("images do not support line ranges")
	}

	loadedRange, err := loadRange(ctx, root, name, args)
	if err != nil {
		return tool.ToolCallResult{Metrics: metrics}, readFailure(args.Path, err)
	}

	metrics.Lines = loadedRange.selectedLines
	metrics.Bytes = int64(len(loadedRange.output))
	snapshot := file.ReadSnapshot{Path: args.Path, Hash: loadedRange.contentHash}
	return successfulSnapshotResult(snapshot, loadedRange.output, tool.Image{}, metrics), nil
}

func readFailure(path string, err error) error {
	if pathError, ok := errors.AsType[*fs.PathError](err); ok {
		return fmt.Errorf("%s: %w", path, pathError.Err)
	}

	return err
}

func loadRange(ctx context.Context, root *file.Root, name string, args Args) (loadedRange, error) {
	openedFile, err := root.Open(name)
	if err != nil {
		return loadedRange{}, err
	}
	defer func() { _ = openedFile.Close() }()

	contentHasher := sha256.New()
	reader := bufio.NewReader(openedFile)
	start := int64(max(args.Offset-1, 0))
	lineIndex := int64(0)
	isLineStart := true
	var output strings.Builder
	var result loadedRange

	for {
		if err := ctx.Err(); err != nil {
			return loadedRange{}, err
		}

		fragment, readErr := reader.ReadSlice('\n')
		if len(fragment) > 0 {
			_, _ = contentHasher.Write(fragment)
		}

		hasLine := len(fragment) > 0 && (readErr == nil || errors.Is(readErr, io.EOF))
		isSelected := lineIndex >= start && (args.Limit <= 0 || result.selectedLines < int64(args.Limit))
		if isSelected && len(fragment) > 0 {
			content := fragment
			if readErr == nil {
				content = content[:len(content)-1]
			}
			separatorBytes := 0
			if isLineStart && result.selectedLines > 0 {
				separatorBytes = 1
			}
			if output.Len()+separatorBytes+len(content) > maxFileBytes {
				return loadedRange{}, fmt.Errorf("range exceeds the %s limit", util.FormatBytes(maxFileBytes, bytePrecision))
			}
			if separatorBytes > 0 {
				output.WriteByte('\n')
			}
			_, _ = output.Write(content)
		}

		if hasLine {
			lineIndex++
			isLineStart = true
		} else {
			isLineStart = false
		}
		if hasLine && isSelected {
			result.selectedLines++
		}
		if readErr != nil && !errors.Is(readErr, bufio.ErrBufferFull) && !errors.Is(readErr, io.EOF) {
			return loadedRange{}, readErr
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}

	result.totalLines = lineIndex
	if result.totalLines > 0 && start >= result.totalLines {
		return result, fmt.Errorf(
			"offset %d exceeds the file's %d lines", args.Offset, result.totalLines,
		)
	}

	result.contentHash = hex.EncodeToString(contentHasher.Sum(nil))
	result.output = output.String()
	return result, nil
}

func load(root *file.Root, name string) (loadedFile, error) {
	openedFile, err := root.Open(name)
	if err != nil {
		return loadedFile{}, err
	}
	defer func() { _ = openedFile.Close() }()

	info, err := openedFile.Stat()
	if err != nil {
		return loadedFile{}, err
	}
	if info.Size() > maxFileBytes {
		header, err := io.ReadAll(io.LimitReader(openedFile, 512))
		if err != nil {
			return loadedFile{}, err
		}
		return loadedFile{
			mediaType: http.DetectContentType(header),
			size:      info.Size(),
		}, errFileTooLarge
	}

	data, err := io.ReadAll(io.LimitReader(openedFile, maxFileBytes+1))
	if err != nil {
		return loadedFile{}, err
	}
	mediaType := http.DetectContentType(data)
	if len(data) > maxFileBytes {
		return loadedFile{
			mediaType: mediaType,
			size:      max(info.Size(), int64(len(data))),
		}, errFileTooLarge
	}

	return loadedFile{data: data, mediaType: mediaType, size: int64(len(data))}, nil
}

func successfulResult(
	path string,
	data []byte,
	output string,
	image tool.Image,
	metrics tool.ToolCallMetrics,
) tool.ToolCallResult {
	return successfulSnapshotResult(file.NewReadSnapshot(path, data), output, image, metrics)
}

func successfulSnapshotResult(
	snapshot file.ReadSnapshot,
	output string,
	image tool.Image,
	metrics tool.ToolCallMetrics,
) tool.ToolCallResult {
	return tool.ToolCallResult{
		State:   file.EncodeReadState(snapshot),
		Output:  output,
		Image:   image,
		Metrics: metrics,
	}
}
