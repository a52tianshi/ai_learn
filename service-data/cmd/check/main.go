package main

import (
	"database/sql"
	"fmt"
	"os"
	"strings"

	_ "github.com/go-sql-driver/mysql"
)

func main() {
	dsn := os.Getenv("MYSQL_DSN")
	if dsn == "" {
		dsn = "root@tcp(127.0.0.1:3306)/wordbot?parseTime=false&charset=utf8mb4"
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		fmt.Printf("Error: 无法连接数据库: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		fmt.Printf("Error: 数据库连接测试失败: %v\n", err)
		os.Exit(1)
	}

	// 如果没有传参数，或者传了 --all / all，则输出全库状态报告
	if len(os.Args) < 2 || os.Args[1] == "--all" || os.Args[1] == "all" {
		printFullReport(db)
		return
	}

	// 否则，按单个单词进行精确查询
	wordText := strings.ToLower(strings.TrimSpace(os.Args[1]))
	printSingleWordReport(db, wordText)
}

func printFullReport(db *sql.DB) {
	fmt.Println("📊 === 单词本数据库全库状态报告 (Full Scan) ===")

	// 1. 词表总数
	var totalWords int
	_ = db.QueryRow("SELECT COUNT(*) FROM words").Scan(&totalWords)
	fmt.Printf("📝 词汇表总词数 (words): %d\n", totalWords)

	// 2. 用户笔记本记录总数
	var totalUserWords int
	_ = db.QueryRow("SELECT COUNT(*) FROM user_words").Scan(&totalUserWords)
	fmt.Printf("👤 用户笔记本总记录数 (user_words): %d\n", totalUserWords)

	// 3. 复习状态分布
	rows, err := db.Query("SELECT status, COUNT(*) FROM user_words GROUP BY status")
	if err == nil {
		defer rows.Close()
		fmt.Println("   └── 📁 状态分布:")
		statusMap := map[int]string{
			0: "新词 (New)",
			1: "学习中 (Learning)",
			2: "已掌握 (Mastered)",
			3: "已搁置/不再复习 (Shelved)",
		}
		counts := make(map[int]int)
		for rows.Next() {
			var status, count int
			if err := rows.Scan(&status, &count); err == nil {
				counts[status] = count
			}
		}
		for s := 0; s <= 3; s++ {
			fmt.Printf("       • %-22s : %d 条\n", statusMap[s], counts[s])
		}
	}

	// 4. 扫描缺失中文释义的单词
	var missingCNCount int
	_ = db.QueryRow(`
		SELECT COUNT(*) FROM words 
		WHERE id NOT IN (
			SELECT DISTINCT word_id FROM word_senses 
			WHERE meaning_cn IS NOT NULL AND meaning_cn <> ''
		)`).Scan(&missingCNCount)
	fmt.Printf("\n⚠️ 缺失中文释义的单词数: %d\n", missingCNCount)

	if missingCNCount > 0 {
		limit := 15
		listRows, err := db.Query(`
			SELECT id, text FROM words 
			WHERE id NOT IN (
				SELECT DISTINCT word_id FROM word_senses 
				WHERE meaning_cn IS NOT NULL AND meaning_cn <> ''
			) 
			LIMIT ?`, limit)
		if err == nil {
			defer listRows.Close()
			fmt.Printf("   └── 📋 缺失中文的单词示例 (前 %d 个):\n", limit)
			for listRows.Next() {
				var id int
				var text string
				if err := listRows.Scan(&id, &text); err == nil {
					fmt.Printf("       - ID: %-4d %s\n", id, text)
				}
			}
			fmt.Println("   💡 提示: 可以使用 `./refresh.sh` 脚本来强制刷新并补全这些单词的中文释义。")
		}
	}
	fmt.Println("\n=============================================")
}

func printSingleWordReport(db *sql.DB, wordText string) {
	// 1. Check words table
	var wordID int64
	var text, phonetic, audioURL string
	err := db.QueryRow(`SELECT id, text, COALESCE(phonetic, ''), COALESCE(audio_url, '') FROM words WHERE text = ?`, wordText).
		Scan(&wordID, &text, &phonetic, &audioURL)
	if err == sql.ErrNoRows {
		fmt.Printf("❌ 单词 '%s' 在 words 表中不存在。\n", wordText)
		return
	} else if err != nil {
		fmt.Printf("查询 words 表出错: %v\n", err)
		return
	}

	fmt.Printf("📌 单词表记录 (words):\n")
	fmt.Printf("  ID: %d\n", wordID)
	fmt.Printf("  单词: %s\n", text)
	fmt.Printf("  音标: %s\n", phonetic)
	fmt.Printf("  发音链接: %s\n\n", audioURL)

	// 2. Check user_words table
	rows, err := db.Query(`SELECT id, tg_user_id, ease_factor, interval_days, repetitions, due_at, COALESCE(last_review_at, 'Never'), status FROM user_words WHERE word_id = ?`, wordID)
	if err != nil {
		fmt.Printf("查询 user_words 表出错: %v\n", err)
		return
	}
	defer rows.Close()

	hasUserWord := false
	fmt.Printf("👤 用户笔记本记录 (user_words):\n")
	for rows.Next() {
		hasUserWord = true
		var uwID, tgUserID int64
		var easeFactor float64
		var intervalDays, repetitions, status int
		var dueAt, lastReviewAt string
		if err := rows.Scan(&uwID, &tgUserID, &easeFactor, &intervalDays, &repetitions, &dueAt, &lastReviewAt, &status); err != nil {
			fmt.Printf("  解析记录行出错: %v\n", err)
			continue
		}

		statusStr := "未知"
		switch status {
		case 0:
			statusStr = "0 (新词 - New)"
		case 1:
			statusStr = "1 (学习中 - Learning)"
		case 2:
			statusStr = "2 (已掌握 - Mastered)"
		case 3:
			statusStr = "3 (已搁置/不再复习 - Shelved)"
		}

		fmt.Printf("  - 用户单词关联 ID (user_word_id): %d\n", uwID)
		fmt.Printf("    Telegram 用户 ID: %d\n", tgUserID)
		fmt.Printf("    记忆简易度 (EF): %.2f\n", easeFactor)
		fmt.Printf("    复习间隔: %d 天\n", intervalDays)
		fmt.Printf("    重复次数: %d\n", repetitions)
		fmt.Printf("    状态: %s\n", statusStr)
		fmt.Printf("    下次复习时间 (due_at): %s\n", dueAt)
		fmt.Printf("    上次复习时间: %s\n\n", lastReviewAt)
	}

	if !hasUserWord {
		fmt.Printf("  暂无任何用户将该单词加入笔记本。\n\n")
	}

	// 3. Check senses
	sensesRows, err := db.Query(`SELECT COALESCE(pos, ''), meaning_en, COALESCE(meaning_cn, '') FROM word_senses WHERE word_id = ?`, wordID)
	if err != nil {
		fmt.Printf("查询 word_senses 表出错: %v\n", err)
		return
	}
	defer sensesRows.Close()

	fmt.Printf("📖 释义内容 (word_senses):\n")
	hasSenses := false
	for sensesRows.Next() {
		hasSenses = true
		var pos, meaningEN, meaningCN string
		if err := sensesRows.Scan(&pos, &meaningEN, &meaningCN); err != nil {
			fmt.Printf("  解析释义行出错: %v\n", err)
			continue
		}
		fmt.Printf("  - [%s] 英文: %s\n", pos, meaningEN)
		if meaningCN != "" {
			fmt.Printf("        中文: %s\n", meaningCN)
		}
	}
	if !hasSenses {
		fmt.Printf("  未找到该单词的任何释义信息。\n")
	}
}
