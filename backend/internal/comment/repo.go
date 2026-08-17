package comment

import (
	"time"

	"gorm.io/gorm"
)

type Repo struct {
	db *gorm.DB
}

func NewRepo(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

func (r *Repo) ArticleExists(articleID uint) (bool, error) {
	var count int64
	if err := r.db.Table("articles").Where("id = ? AND status = ?", articleID, "published").Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *Repo) Create(comment *Comment) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(comment).Error; err != nil {
			return err
		}

		return tx.Table("articles").
			Where("id = ?", comment.ArticleID).
			UpdateColumn("comment_count", gorm.Expr("comment_count + ?", 1)).
			Error
	})
}

func (r *Repo) FindByID(commentID uint) (*Comment, error) {
	var comment Comment
	if err := r.db.First(&comment, commentID).Error; err != nil {
		return nil, err
	}
	return &comment, nil
}

func (r *Repo) Delete(comment *Comment) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(comment).Error; err != nil {
			return err
		}

		return tx.Table("articles").
			Where("id = ?", comment.ArticleID).
			UpdateColumn("comment_count", gorm.Expr("CASE WHEN comment_count > 0 THEN comment_count - 1 ELSE 0 END")).
			Error
	})
}

func (r *Repo) FindAuthorByID(userID uint) (CommentAuthorResponse, error) {
	type userRow struct {
		ID       uint
		Username string
	}

	var row userRow
	if err := r.db.Table("users").Select("id, username").Where("id = ?", userID).Take(&row).Error; err != nil {
		return CommentAuthorResponse{}, err
	}

	return CommentAuthorResponse{
		ID:       row.ID,
		Username: row.Username,
	}, nil
}

func (r *Repo) ListByArticleID(articleID uint) ([]CommentResponse, error) {
	rows := make([]struct {
		ID             uint
		ArticleID      uint
		UserID         uint
		Content        string
		CommentCreated time.Time `gorm:"column:comment_created_at"`
		CommentUpdated time.Time `gorm:"column:comment_updated_at"`
		AuthorID       uint      `gorm:"column:author_id"`
		AuthorUsername string    `gorm:"column:author_username"`
	}, 0)

	err := r.db.Table("comments").
		Select(
			"comments.id, comments.article_id, comments.user_id, comments.content, "+
				"comments.created_at AS comment_created_at, comments.updated_at AS comment_updated_at, "+
				"users.id AS author_id, users.username AS author_username",
		).
		Joins("JOIN articles ON articles.id = comments.article_id AND articles.status = ?", "published").
		Joins("LEFT JOIN users ON users.id = comments.user_id").
		Where("comments.article_id = ?", articleID).
		Order("comments.created_at ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	responses := make([]CommentResponse, 0, len(rows))
	for _, row := range rows {
		responses = append(responses, CommentResponse{
			ID:        row.ID,
			ArticleID: row.ArticleID,
			UserID:    row.UserID,
			Content:   row.Content,
			Author: CommentAuthorResponse{
				ID:       row.AuthorID,
				Username: row.AuthorUsername,
			},
			CreatedAt: row.CommentCreated,
			UpdatedAt: row.CommentUpdated,
		})
	}

	return responses, nil
}
