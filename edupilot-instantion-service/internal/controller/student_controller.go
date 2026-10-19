package controller

import (
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/edupilot-instantion-service/internal/apperr"
	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/edupilot-instantion-service/internal/middleware"
	"github.com/edupilot-instantion-service/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

var (
	studentService         = service.NewStudentService(nil)
	studentHistoryService  = service.NewStudentHistoryService(nil)
	studentDocumentService = service.NewStudentDocumentService(nil)
	studentExcelService    = service.NewStudentExcelService(nil)
)

// ==================== Students CRUD & Stats ====================

func GetStudents(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}

	yearID, gradeID, classID, ok := studentFilterIDs(c)
	if !ok {
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))

	students, meta, err := studentService.ListStudents(c.Request.Context(), schoolID, service.StudentListQuery{
		Search:         c.Query("search"),
		Status:         c.Query("status"),
		Gender:         c.Query("gender"),
		AcademicYearID: yearID,
		GradeLevelID:   gradeID,
		ClassID:        classID,
		Unassigned:     c.Query("unassigned") == "true",
		Page:           page,
		Limit:          limit,
		Sort:           c.Query("sort"),
	})
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": students, "meta": meta})
}

func GetStudentStats(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	stats, err := studentService.GetStats(c.Request.Context(), schoolID)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": stats})
}

func GetStudent(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID siswa")
	if !ok {
		return
	}

	st, err := studentService.GetStudent(c.Request.Context(), schoolID, id)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": st})
}

func CreateStudent(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	var req dto.StudentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	st, err := studentService.CreateStudent(c.Request.Context(), schoolID, middleware.UserID(c), req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": st})
}

func UpdateStudent(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID siswa")
	if !ok {
		return
	}

	var req dto.StudentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	st, err := studentService.UpdateStudent(c.Request.Context(), schoolID, id, req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": st})
}

func DeleteStudent(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID siswa")
	if !ok {
		return
	}

	if err := studentService.DeleteStudent(c.Request.Context(), schoolID, id); err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func UpdateStudentStatus(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID siswa")
	if !ok {
		return
	}

	var req dto.StudentStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	if err := studentService.UpdateStudentStatus(c.Request.Context(), schoolID, id, req); err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Status siswa berhasil diperbarui"})
}

func UploadStudentPhoto(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID siswa")
	if !ok {
		return
	}

	data, mimeType, _, ok := readUpload(c, "photo", maxStudentUpload, allowedPhotoTypes, "Format foto harus PNG, JPEG, atau WEBP")
	if !ok {
		return
	}
	dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(data))
	if err := studentService.UpdatePhoto(c.Request.Context(), schoolID, id, dataURL); err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": gin.H{"photo": dataURL}})
}

func DeleteStudentPhoto(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID siswa")
	if !ok {
		return
	}

	if err := studentService.DeletePhoto(c.Request.Context(), schoolID, id); err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ==================== Documents ====================

func GetStudentDocuments(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID siswa")
	if !ok {
		return
	}

	docs, err := studentDocumentService.GetStudentDocuments(c.Request.Context(), schoolID, id)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": docs})
}

func UploadStudentDocument(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID siswa")
	if !ok {
		return
	}

	docTypeID, err := uuid.Parse(strings.TrimSpace(c.PostForm("document_type_id")))
	if err != nil {
		if isBodyTooLarge(err) {
			respondError(c, errFileTooLarge)
			return
		}
		respondError(c, apperr.Validation("ID jenis dokumen tidak valid", map[string]string{"document_type_id": "ID jenis dokumen tidak valid"}))
		return
	}

	data, mimeType, fileName, ok := readUpload(c, "file", maxStudentUpload, allowedDocumentTypes, "Format berkas harus PDF, PNG, atau JPEG")
	if !ok {
		return
	}
	dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(data))
	notes := c.PostForm("notes")

	doc, err := studentDocumentService.CreateStudentDocument(c.Request.Context(), schoolID, id, docTypeID, fileName, mimeType, notes, dataURL, int64(len(data)))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": doc})
}

func DownloadStudentDocument(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID siswa")
	if !ok {
		return
	}
	docID, ok := parseUUIDParam(c, "docId", "ID dokumen")
	if !ok {
		return
	}

	data, fileName, mimeType, err := studentDocumentService.GetDocumentForDownload(c.Request.Context(), schoolID, id, docID)
	if err != nil {
		respondError(c, err)
		return
	}

	c.Header("Content-Disposition", contentDisposition(fileName))
	c.Header("Cache-Control", "private, no-store")
	c.Data(http.StatusOK, mimeType, data)
}

func DeleteStudentDocument(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID siswa")
	if !ok {
		return
	}
	docID, ok := parseUUIDParam(c, "docId", "ID dokumen")
	if !ok {
		return
	}

	if err := studentDocumentService.DeleteDocument(c.Request.Context(), schoolID, id, docID); err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ==================== History & Placement ====================

func GetStudentHistory(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID siswa")
	if !ok {
		return
	}

	histories, err := studentHistoryService.GetStudentHistory(c.Request.Context(), schoolID, id)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": histories})
}

func PlaceStudent(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID siswa")
	if !ok {
		return
	}

	var req dto.PlacementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	if err := studentHistoryService.PlaceStudent(c.Request.Context(), schoolID, id, middleware.UserID(c), req); err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Penempatan kelas berhasil"})
}

func AddClassMembers(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	classID, ok := parseUUIDParam(c, "id", "ID kelas")
	if !ok {
		return
	}

	var req dto.MembersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	res, err := studentHistoryService.BulkAddClassMembers(c.Request.Context(), schoolID, classID, middleware.UserID(c), req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": res})
}

func RemoveClassMembers(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	classID, ok := parseUUIDParam(c, "id", "ID kelas")
	if !ok {
		return
	}

	var req struct {
		StudentIDs []uuid.UUID `json:"student_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	removed, err := studentHistoryService.BulkRemoveClassMembers(c.Request.Context(), schoolID, classID, req.StudentIDs)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"removed": removed}})
}

func PreviewPromotions(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	fromClassID, err := uuid.Parse(c.Query("from_class_id"))
	if err != nil {
		respondError(c, apperr.BadRequest("ID kelas asal tidak valid"))
		return
	}
	toAcademicYearID, err := uuid.Parse(c.Query("to_academic_year_id"))
	if err != nil {
		respondError(c, apperr.BadRequest("ID tahun ajaran tujuan tidak valid"))
		return
	}

	preview, err := studentHistoryService.PreviewPromotions(c.Request.Context(), schoolID, fromClassID, toAcademicYearID)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": preview})
}

func ExecutePromotions(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	var req dto.PromotionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	res, err := studentHistoryService.ExecutePromotions(c.Request.Context(), schoolID, middleware.UserID(c), req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": res})
}

// ==================== Import & Export ====================

func DownloadStudentTemplate(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	data, err := studentExcelService.GenerateTemplate(c.Request.Context(), schoolID)
	if err != nil {
		respondError(c, err)
		return
	}

	c.Header("Content-Disposition", `attachment; filename="template_siswa_edupilot.xlsx"`)
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", data)
}

func ImportStudents(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	// Default is a dry run; only dry_run=false writes.
	dryRun := strings.ToLower(c.DefaultQuery("dry_run", "true")) != "false"
	mode := strings.ToLower(c.DefaultQuery("mode", "create")) // create or upsert

	fh, err := c.FormFile("file")
	if err != nil {
		if isBodyTooLarge(err) {
			respondError(c, apperr.New(http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "Ukuran file maksimal 10 MB"))
			return
		}
		respondError(c, apperr.Validation("File Excel wajib diunggah (field: file)", map[string]string{"file": "File wajib diunggah"}))
		return
	}
	src, err := fh.Open()
	if err != nil {
		respondError(c, fmt.Errorf("open uploaded import: %w", err))
		return
	}
	defer src.Close()

	res, err := studentExcelService.ImportStudents(c.Request.Context(), schoolID, middleware.UserID(c), fh.Filename, fh.Size, src, dryRun, mode)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": res})
}

func ExportStudents(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}

	yearID, gradeID, classID, ok := studentFilterIDs(c)
	if !ok {
		return
	}

	data, err := studentExcelService.ExportStudents(c.Request.Context(), schoolID, service.StudentListQuery{
		Search:         c.Query("search"),
		Status:         c.Query("status"),
		Gender:         c.Query("gender"),
		AcademicYearID: yearID,
		GradeLevelID:   gradeID,
		ClassID:        classID,
		Unassigned:     c.Query("unassigned") == "true",
		Sort:           c.Query("sort"),
	})
	if err != nil {
		respondError(c, err)
		return
	}

	fileName := fmt.Sprintf("data_siswa_%s.xlsx", time.Now().Format("20060102_150405"))
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", data)
}

// GetClassStudents is the class roster (§8.5): paginated StudentSummary of the
// students with an active history in the class.
func GetClassStudents(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	classID, ok := parseUUIDParam(c, "id", "ID kelas")
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	students, meta, err := studentService.ListClassStudents(c.Request.Context(), schoolID, classID, service.StudentListQuery{
		Search: c.Query("search"),
		Status: c.Query("status"),
		Gender: c.Query("gender"),
		Page:   page,
		Limit:  limit,
		Sort:   c.Query("sort"),
	})
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": students, "meta": meta})
}

func studentFilterIDs(c *gin.Context) (yearID, gradeID, classID *uuid.UUID, ok bool) {
	if yearID, ok = optionalUUIDQuery(c, "academic_year_id"); !ok {
		return
	}
	if gradeID, ok = optionalUUIDQuery(c, "grade_level_id"); !ok {
		return
	}
	classID, ok = optionalUUIDQuery(c, "class_id")
	return
}

const maxStudentUpload = 2 << 20 // 2 MB (§8.6, §8.7)

var (
	allowedPhotoTypes    = map[string]bool{"image/png": true, "image/jpeg": true, "image/webp": true}
	allowedDocumentTypes = map[string]bool{"application/pdf": true, "image/png": true, "image/jpeg": true}
	errFileTooLarge      = apperr.New(http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "Ukuran file maksimal 2 MB")
)

// readUpload reads a multipart file (bounded), sniffs its real content type
// (client Content-Type is ignored) and returns data, mime and a safe base name.
func readUpload(c *gin.Context, field string, maxSize int64, allowed map[string]bool, typeMsg string) ([]byte, string, string, bool) {
	fh, err := c.FormFile(field)
	if err != nil {
		if isBodyTooLarge(err) {
			respondError(c, errFileTooLarge)
			return nil, "", "", false
		}
		respondError(c, apperr.Validation(fmt.Sprintf("File wajib diunggah (field: %s)", field), map[string]string{field: "File wajib diunggah"}))
		return nil, "", "", false
	}
	if fh.Size > maxSize {
		respondError(c, errFileTooLarge)
		return nil, "", "", false
	}
	src, err := fh.Open()
	if err != nil {
		respondError(c, fmt.Errorf("open upload: %w", err))
		return nil, "", "", false
	}
	defer src.Close()
	data, err := io.ReadAll(io.LimitReader(src, maxSize+1))
	if err != nil {
		respondError(c, fmt.Errorf("read upload: %w", err))
		return nil, "", "", false
	}
	if int64(len(data)) > maxSize {
		respondError(c, errFileTooLarge)
		return nil, "", "", false
	}
	if len(data) == 0 {
		respondError(c, apperr.Validation("File kosong", map[string]string{field: "File kosong"}))
		return nil, "", "", false
	}
	mimeType := http.DetectContentType(data)
	if i := strings.Index(mimeType, ";"); i >= 0 {
		mimeType = mimeType[:i]
	}
	if !allowed[mimeType] {
		respondError(c, apperr.New(http.StatusUnsupportedMediaType, "INVALID_FILE_TYPE", typeMsg))
		return nil, "", "", false
	}
	return data, mimeType, safeFileName(fh.Filename), true
}

// safeFileName strips paths and control/quote characters and bounds length.
func safeFileName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '"' || r == '/' || r == '\\' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." {
		name = "dokumen"
	}
	if r := []rune(name); len(r) > 200 {
		name = string(r[len(r)-200:])
	}
	return name
}

// contentDisposition builds an attachment header safe for any file name
// (RFC 6266 / 2231 encoding for non-ASCII).
func contentDisposition(name string) string {
	if v := mime.FormatMediaType("attachment", map[string]string{"filename": safeFileName(name)}); v != "" {
		return v
	}
	return `attachment; filename="dokumen"`
}
