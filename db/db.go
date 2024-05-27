package db

import (
	"database/sql"
	_ "github.com/go-sql-driver/mysql"
	"log"
	"os"
)

var db *sql.DB

func InitDB() {
	user := os.Getenv("DB_USER")
	password := os.Getenv("DB_PASSWORD")

	var err error
	db, err = sql.Open("mysql", user+":"+password+"@tcp(127.0.0.1:3306)/arrakis")
	if err != nil {
		panic(err.Error())
	}

	log.Println("[INFO] connected to database")
}

func CloseDB() {
	db.Close()
}

func SetAnnouncementChannel(guildID string, channelID string) {
	_, err := db.Exec("INSERT INTO announcement_channels (guild_id, channel_id) VALUES (?, ?) ON DUPLICATE KEY UPDATE channel_id = ?", guildID, channelID, channelID)
	if err != nil {
		panic(err.Error())
	}
}
