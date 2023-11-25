package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo-contrib/session"
	"github.com/labstack/echo/v4"
)

type ReactionModel struct {
	ID           int64  `db:"id"`
	EmojiName    string `db:"emoji_name"`
	UserID       int64  `db:"user_id"`
	LivestreamID int64  `db:"livestream_id"`
	CreatedAt    int64  `db:"created_at"`
}

type Reaction struct {
	ID         int64      `json:"id"`
	EmojiName  string     `json:"emoji_name"`
	User       User       `json:"user"`
	Livestream Livestream `json:"livestream"`
	CreatedAt  int64      `json:"created_at"`
}

type PostReactionRequest struct {
	EmojiName string `json:"emoji_name"`
}

func getReactionsHandler(c echo.Context) error {
	ctx := c.Request().Context()

	if err := verifyUserSession(c); err != nil {
		// echo.NewHTTPErrorが返っているのでそのまま出力
		return err
	}

	livestreamID, err := strconv.Atoi(c.Param("livestream_id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "livestream_id in path must be integer")
	}

	tx, err := dbConn.BeginTxx(ctx, nil)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to begin transaction: "+err.Error())
	}
	defer tx.Rollback()

	query := "SELECT * FROM reactions WHERE livestream_id = ? ORDER BY created_at DESC"
	if c.QueryParam("limit") != "" {
		limit, err := strconv.Atoi(c.QueryParam("limit"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "limit query parameter must be integer")
		}
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	reactionModels := []ReactionModel{}
	if err := tx.SelectContext(ctx, &reactionModels, query, livestreamID); err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "failed to get reactions")
	}

	reactions, err := fillReactionResponses(ctx, tx, reactionModels)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to fill reactions: "+err.Error())
	}

	if err := tx.Commit(); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to commit: "+err.Error())
	}

	return c.JSON(http.StatusOK, reactions)
}

func postReactionHandler(c echo.Context) error {
	ctx := c.Request().Context()
	livestreamID, err := strconv.Atoi(c.Param("livestream_id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "livestream_id in path must be integer")
	}

	if err := verifyUserSession(c); err != nil {
		// echo.NewHTTPErrorが返っているのでそのまま出力
		return err
	}

	// error already checked
	sess, _ := session.Get(defaultSessionIDKey, c)
	// existence already checked
	userID := sess.Values[defaultUserIDKey].(int64)

	var req *PostReactionRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "failed to decode the request body as json")
	}

	tx, err := dbConn.BeginTxx(ctx, nil)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to begin transaction: "+err.Error())
	}
	defer tx.Rollback()

	reactionModel := ReactionModel{
		UserID:       int64(userID),
		LivestreamID: int64(livestreamID),
		EmojiName:    req.EmojiName,
		CreatedAt:    time.Now().Unix(),
	}

	result, err := tx.NamedExecContext(ctx, "INSERT INTO reactions (user_id, livestream_id, emoji_name, created_at) VALUES (:user_id, :livestream_id, :emoji_name, :created_at)", reactionModel)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to insert reaction: "+err.Error())
	}

	reactionID, err := result.LastInsertId()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get last inserted reaction id: "+err.Error())
	}
	reactionModel.ID = reactionID

	reaction, err := fillReactionResponse(ctx, tx, reactionModel)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to fill reaction: "+err.Error())
	}

	if err := tx.Commit(); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to commit: "+err.Error())
	}

	return c.JSON(http.StatusCreated, reaction)
}

func fillReactionResponse(ctx context.Context, tx *sqlx.Tx, reactionModel ReactionModel) (Reaction, error) {
	userModel := UserModel{}
	if err := tx.GetContext(ctx, &userModel, "SELECT * FROM users WHERE id = ?", reactionModel.UserID); err != nil {
		return Reaction{}, err
	}
	user, err := fillUserResponse(ctx, tx, userModel)
	if err != nil {
		return Reaction{}, err
	}

	livestreamModel := LivestreamModel{}
	if err := tx.GetContext(ctx, &livestreamModel, "SELECT * FROM livestreams WHERE id = ?", reactionModel.LivestreamID); err != nil {
		return Reaction{}, err
	}
	livestream, err := fillLivestreamResponse(ctx, tx, livestreamModel)
	if err != nil {
		return Reaction{}, err
	}

	reaction := Reaction{
		ID:         reactionModel.ID,
		EmojiName:  reactionModel.EmojiName,
		User:       user,
		Livestream: livestream,
		CreatedAt:  reactionModel.CreatedAt,
	}

	return reaction, nil
}

func fillReactionResponses(ctx context.Context, tx *sqlx.Tx, reactionModels []ReactionModel) ([]Reaction, error) {
	if len(reactionModels) == 0 {
		return []Reaction{}, nil
	}

	userIDs := make([]int64, len(reactionModels))
	for _, reaction := range reactionModels {
		userIDs = append(userIDs, reaction.UserID)
	}
	userResps, err := fillUserResponses(ctx, tx, userIDs)
	if err != nil {
		return nil, err
	}

	livestreamIDs := make([]int64, len(reactionModels))
	for _, reaction := range reactionModels {
		livestreamIDs = append(livestreamIDs, reaction.LivestreamID)
	}
	var livestreamModels []*LivestreamModel
	query, params, err := sqlx.In("SELECT * FROM livestreams WHERE id IN (?)", livestreamIDs)
	if err != nil {
		return nil, err
	}
	if err := tx.SelectContext(ctx, &livestreamModels, query, params...); err != nil {
		return nil, err
	}
	livestreamResps, err := fillLivestreamResponses(ctx, tx, livestreamModels)
	if err != nil {
		return nil, err
	}
	livestreamMap := make(map[int64]Livestream, len(livestreamResps))
	for _, resp := range livestreamResps {
		livestreamMap[resp.ID] = resp
	}

	reactions := make([]Reaction, len(reactionModels))
	for i := range reactionModels {
		user := userResps[reactionModels[i].UserID]
		livestream := livestreamMap[reactionModels[i].LivestreamID]

		reaction := Reaction{
			ID:         reactionModels[i].ID,
			EmojiName:  reactionModels[i].EmojiName,
			User:       user,
			Livestream: livestream,
			CreatedAt:  reactionModels[i].CreatedAt,
		}

		reactions[i] = reaction
	}

	return reactions, nil
}

func fillUserResponses(ctx context.Context, tx *sqlx.Tx, userIDs []int64) (map[int64]User, error) {
	userModels := []UserModel{}
	query, params, err := sqlx.In("SELECT * FROM users WHERE id IN (?)", userIDs)
	if err != nil {
		return nil, err
	}
	if err := tx.SelectContext(ctx, &userModels, query, params...); err != nil {
		return nil, err
	}
	userMap := make(map[int64]UserModel, len(userModels))
	for _, ownerModel := range userModels {
		userMap[ownerModel.ID] = ownerModel
	}

	themeModels := []ThemeModel{}
	query, params, err = sqlx.In("SELECT * FROM themes WHERE user_id IN (?)", userIDs)
	if err != nil {
		return nil, err
	}
	if err := tx.SelectContext(ctx, &themeModels, query, params...); err != nil {
		return nil, err
	}

	themeMap := make(map[int64]ThemeModel, len(themeModels))
	for _, themeModel := range themeModels {
		themeMap[themeModel.UserID] = themeModel
	}

	var iconHashes []struct {
		UserID int64  `db:"user_id"`
		Hash   string `db:"hash"`
	}
	query, params, err = sqlx.In("SELECT i.user_id, ih.hash FROM icon_hashes AS ih JOIN icons AS i ON i.id = ih.icon_id WHERE i.user_id IN (?)", userIDs)
	if err != nil {
		return nil, err
	}
	if err := tx.SelectContext(ctx, &iconHashes, query, params...); err != nil {
		return nil, err
	}
	hashMap := make(map[int64]string, len(iconHashes))
	for _, iconHash := range iconHashes {
		hashMap[iconHash.UserID] = iconHash.Hash
	}

	userResponseMap := make(map[int64]User, len(userIDs))
	for _, id := range userIDs {
		iconHash, ok := hashMap[id]
		if !ok {
			iconHash = fallbackImageHash
		}
		userResponseMap[id] = User{
			ID:          userMap[id].ID,
			Name:        userMap[id].Name,
			DisplayName: userMap[id].DisplayName,
			Description: userMap[id].Description,
			Theme: Theme{
				ID:       themeMap[id].ID,
				DarkMode: themeMap[id].DarkMode,
			},
			IconHash: iconHash,
		}
	}

	return userResponseMap, nil
}
