package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"SysDashServer/internal/api/middleware"
	"SysDashServer/internal/auth"
	"SysDashServer/internal/database"
	"SysDashServer/internal/models"

	"github.com/gin-gonic/gin"
)

type UserHandler struct {
	db *database.DB
}

func NewUserHandler(db *database.DB) *UserHandler {
	return &UserHandler{db: db}
}

// ListUsers returns filtered, searched and paginated users (Admin only)
// Query params:
// - role: "admin", "viewer", or "" (all)
// - search: substring match on username or full_name
// - page: 1-based page number (default 1)
// - limit: items per page (default 20, max 100)
func (h *UserHandler) ListUsers(c *gin.Context) {
	roleParam := c.Query("role")
	search := strings.TrimSpace(c.Query("search"))

	pageStr := c.DefaultQuery("page", "1")
	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		page = 1
	}

	limitStr := c.DefaultQuery("limit", "20")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 || limit > 100 {
		limit = 20
	}

	query := h.db.Model(&models.User{})

	if roleParam != "" {
		query = query.Where("LOWER(role) = ?", strings.ToLower(roleParam))
	}

	if search != "" {
		pattern := "%" + strings.ToLower(search) + "%"
		query = query.Where("LOWER(username) LIKE ? OR LOWER(full_name) LIKE ?", pattern, pattern)
	}

	var totalCount int64
	query.Count(&totalCount)

	offset := (page - 1) * limit
	var users []models.User
	if err := query.Order("id asc").Offset(offset).Limit(limit).Find(&users).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query users"})
		return
	}

	totalPages := int((totalCount + int64(limit) - 1) / int64(limit))
	if totalPages == 0 {
		totalPages = 1
	}

	c.JSON(http.StatusOK, gin.H{
		"page":        page,
		"limit":       limit,
		"total":       totalCount,
		"total_pages": totalPages,
		"users":       users,
	})
}

func (h *UserHandler) CreateUser(c *gin.Context) {
	var req models.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body", "details": err.Error()})
		return
	}

	var existing models.User
	if err := h.db.Where("username = ?", req.Username).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "username already exists"})
		return
	}

	hashedPassword, err := auth.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
		return
	}

	role := models.RoleViewer
	if req.Role == models.RoleAdmin || req.Role == models.RoleViewer {
		role = req.Role
	}

	user := models.User{
		Username: req.Username,
		Password: hashedPassword,
		FullName: req.FullName,
		Role:     role,
	}

	if err := h.db.Create(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create user"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "user created successfully", "user": user})
}

func (h *UserHandler) UpdateUser(c *gin.Context) {
	idParam := c.Param("id")
	id, err := strconv.ParseUint(idParam, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}

	var user models.User
	if err := h.db.First(&user, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	var req models.UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	if req.FullName != "" {
		user.FullName = req.FullName
	}
	if req.Role != "" && (req.Role == models.RoleAdmin || req.Role == models.RoleViewer) {
		user.Role = req.Role
	}
	if req.Password != "" {
		if hashed, err := auth.HashPassword(req.Password); err == nil {
			user.Password = hashed
		}
	}

	if err := h.db.Save(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update user"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "user updated successfully", "user": user})
}

func (h *UserHandler) DeleteUser(c *gin.Context) {
	idParam := c.Param("id")
	id, err := strconv.ParseUint(idParam, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}

	// Prevent user from deleting themselves
	currentUserID, _ := c.Get(middleware.CtxUserID)
	if currentUserID == uint(id) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot delete your own account"})
		return
	}

	if err := h.db.Delete(&models.User{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete user"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "user deleted successfully"})
}
