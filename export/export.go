package export

import (
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/session"
	"github.com/detailyang/pig/sessionexport"
)

func Render(sess *session.Session) (string, error) {
	return sessionexport.Render(sess)
}

func RenderContext(ctx session.Context) string {
	return sessionexport.RenderContext(ctx)
}

func RenderEntries(entries []session.Entry) string {
	return sessionexport.RenderEntries(entries)
}

func RenderUserContent(content ai.UserContent) string {
	return sessionexport.RenderUserContent(content)
}

func DefaultExportPath(sessionID string) string {
	return sessionexport.DefaultExportPath(sessionID)
}

func SaveContext(ctx session.Context, dest string) (string, error) {
	return sessionexport.SaveContext(ctx, dest)
}

func Save(sess *session.Session, dest string) (string, error) {
	return sessionexport.Save(sess, dest)
}
