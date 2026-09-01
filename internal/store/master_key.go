package store

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"

	"github.com/hkjang/jikim/internal/cryptox"
	"github.com/jackc/pgx/v5"
)

const masterKeySentinel = "jikim-master-key-sentinel:v1"

func (s *Store) VerifyMasterKey(ctx context.Context) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('jikim_master_key_sentinel'))`); err != nil {
		return err
	}
	var ciphertext, nonce []byte
	err = tx.QueryRow(ctx, `SELECT ciphertext,nonce FROM system_crypto_sentinel WHERE singleton=true FOR UPDATE`).Scan(&ciphertext, &nonce)
	if errors.Is(err, pgx.ErrNoRows) {
		ciphertext, nonce, err = s.master.Encrypt([]byte(masterKeySentinel), []byte("master-key-sentinel"))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO system_crypto_sentinel(singleton,ciphertext,nonce) VALUES(true,$1,$2)`, ciphertext, nonce); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if err := verifyMasterKeySentinel(s.master, ciphertext, nonce); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func verifyMasterKeySentinel(cipher *cryptox.Cipher, ciphertext, nonce []byte) error {
	plaintext, err := cipher.Decrypt(ciphertext, nonce, []byte("master-key-sentinel"))
	if err != nil {
		return fmt.Errorf("ENCRYPTION_KEY가 기존 데이터베이스의 키와 일치하지 않습니다: %w", err)
	}
	if subtle.ConstantTimeCompare(plaintext, []byte(masterKeySentinel)) != 1 {
		return errors.New("ENCRYPTION_KEY sentinel 값이 올바르지 않습니다")
	}
	return nil
}
