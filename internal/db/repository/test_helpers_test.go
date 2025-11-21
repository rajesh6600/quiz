package repository

import "github.com/jackc/pgx/v5/pgtype"

func uuidFromByte(b byte) pgtype.UUID {
	var arr [16]byte
	arr[15] = b
	return pgtype.UUID{Bytes: arr, Valid: true}
}
