package db

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var Dao *gorm.DB
var CurrentSQLitePath string

func Init(sqlitePath string) {
	dbLogger := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold:             time.Second * 3,
			Colorful:                  false,
			IgnoreRecordNotFoundError: true,
			ParameterizedQueries:      false,
			LogLevel:                  logger.Silent,
		},
	)
	var openDb *gorm.DB
	var err error
	if sqlitePath == "" {
		sqlitePath = defaultSQLiteDSN()
	}
	openDb, err = gorm.Open(sqlite.Open(sqlitePath), &gorm.Config{
		Logger:                                   dbLogger,
		DisableForeignKeyConstraintWhenMigrating: true,
		SkipDefaultTransaction:                   true,
		PrepareStmt:                              true,
	})

	if err != nil {
		log.Fatalf("db connection error is %s", err.Error())
	}
	CurrentSQLitePath = stripSQLiteDSN(sqlitePath)

	// 兜底：确保 busy_timeout / WAL / synchronous 生效（不同驱动/DSN 参数支持可能存在差异）
	_ = openDb.Exec("PRAGMA busy_timeout=10000").Error
	_ = openDb.Exec("PRAGMA journal_mode=WAL").Error
	_ = openDb.Exec("PRAGMA synchronous=NORMAL").Error

	dbCon, err := openDb.DB()
	if err != nil {
		log.Fatalf("openDb.DB error is  %s", err.Error())
	}
	// SQLite 写入是串行锁模型：连接开太多会放大锁竞争导致 SQLITE_BUSY
	dbCon.SetMaxIdleConns(1)
	dbCon.SetMaxOpenConns(5)
	dbCon.SetConnMaxLifetime(time.Hour)
	Dao = openDb
	AutoMigrate()
}

func defaultSQLiteDSN() string {
	dbPath := resolveDefaultSQLitePath()
	return dbPath + "?_busy_timeout=10000&_journal_mode=WAL&_synchronous=NORMAL&_cache_size=-524288"
}

func resolveDefaultSQLitePath() string {
	if envPath := strings.TrimSpace(os.Getenv("GO_STOCK_DB_PATH")); envPath != "" {
		if err := os.MkdirAll(filepath.Dir(envPath), os.ModePerm); err != nil {
			log.Printf("create GO_STOCK_DB_PATH dir failed: %v", err)
		}
		return envPath
	}

	localPath, _ := filepath.Abs(filepath.Join("data", "stock.db"))
	userPath := filepath.Join(userDataDir(), "stock.db")

	if _, err := os.Stat(localPath); err == nil {
		if isSQLitePathWritable(localPath) {
			log.Printf("using writable sqlite database: %s", localPath)
			return localPath
		}
		if err := copySQLiteDatabase(localPath, userPath); err != nil {
			log.Printf("copy readonly sqlite database to user data dir failed: %v", err)
		} else {
			log.Printf("local sqlite database is readonly, using user data copy: %s", userPath)
		}
		return userPath
	}

	if err := os.MkdirAll(filepath.Dir(userPath), os.ModePerm); err != nil {
		log.Printf("create user sqlite dir failed: %v", err)
	}
	log.Printf("using user sqlite database: %s", userPath)
	return userPath
}

func userDataDir() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base, err = os.UserHomeDir()
		if err != nil || base == "" {
			base = "."
		}
	}
	return filepath.Join(base, "go-stock", "data")
}

func isSQLitePathWritable(dbPath string) bool {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return false
	}
	temp, err := os.CreateTemp(dir, ".write-test-*")
	if err != nil {
		return false
	}
	temp.Close()
	_ = os.Remove(temp.Name())

	if _, err := os.Stat(dbPath); err == nil {
		f, err := os.OpenFile(dbPath, os.O_RDWR, 0)
		if err != nil {
			return false
		}
		_ = f.Close()
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		sidecar := dbPath + suffix
		if _, err := os.Stat(sidecar); err == nil {
			f, err := os.OpenFile(sidecar, os.O_RDWR, 0)
			if err != nil {
				return false
			}
			_ = f.Close()
		}
	}
	return true
}

func copySQLiteDatabase(srcDB, dstDB string) error {
	if err := os.MkdirAll(filepath.Dir(dstDB), os.ModePerm); err != nil {
		return err
	}
	if _, err := os.Stat(dstDB); err == nil && isSQLitePathWritable(dstDB) {
		return nil
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		src := srcDB + suffix
		dst := dstDB + suffix
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := copyFile(src, dst); err != nil {
			return fmt.Errorf("copy %s to %s: %w", src, dst, err)
		}
		_ = os.Chmod(dst, 0666)
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func stripSQLiteDSN(dsn string) string {
	if i := strings.Index(dsn, "?"); i >= 0 {
		return dsn[:i]
	}
	return dsn
}
