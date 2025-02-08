package ydb_adapter

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/bratushkadan/floral/internal/auth/core/domain"
	"github.com/bratushkadan/floral/pkg/resource/idhash"
	"github.com/bratushkadan/floral/pkg/template"
	"github.com/ydb-platform/ydb-go-sdk/v3"
	"github.com/ydb-platform/ydb-go-sdk/v3/table"
	"github.com/ydb-platform/ydb-go-sdk/v3/table/result/named"
	"github.com/ydb-platform/ydb-go-sdk/v3/table/types"
	"go.uber.org/zap"
)

type Token struct {
	db       *ydb.Driver
	l        *zap.Logger
	idHasher idhash.IdHasher
}

var _ domain.RefreshTokenProvider = (*Token)(nil)

type TokenConf struct {
	DbDriver *ydb.Driver
	Logger   *zap.Logger
}

func NewToken(conf TokenConf) *Account {
	adapter := &Account{
		db: conf.DbDriver,
	}

	if conf.Logger == nil {
		adapter.l = zap.NewNop()
	}

	return adapter
}

const RefreshTokensIssuedLimitation = 10

var queryGetRefreshTokens = template.ReplaceAllPairs(`
DECLARE $account_id AS Utf8;

SELECT 
    id,
    created_at,
    expires_at
FROM
    {{table.refresh_tokens}}
VIEW
    {{index.account_id}}
WHERE
    account_id = $account_id
ORDER BY created_at DESC
LIMIT {{tokens_count_limitation}};
`,
	"{{table.refresh_tokens}}", tableRefreshTokens,
	"{{index.account_id}}", tableRefreshTokensIndexAccountId,
	"{{tokens_count_limitation}}", strconv.Itoa(RefreshTokensIssuedLimitation),
)

func (p *Token) Get(ctx context.Context, in domain.RefreshTokenGetDTOInput) (domain.RefreshTokenGetDTOOutput, error) {
	outTokens := make([]domain.RefreshTokenGetDTOOutputToken, 0)

	if err := p.db.Table().DoTx(ctx, func(ctx context.Context, tx table.TransactionActor) error {
		res, err := tx.Execute(ctx, queryDeleteRefreshTokensByAccountId, table.NewQueryParameters(
			table.ValueParam("$account_id", types.UTF8Value(in.AccountId)),
		))
		if err != nil {
			return err
		}
		if err := res.Err(); err != nil {
			return err
		}
		defer func() {
			if err := res.Close(); err != nil {
				p.l.Error("failed to close ydb result", zap.Error(err))
			}
		}()

		for res.NextResultSet(ctx) {
			for res.NextRow() {
				var outToken domain.RefreshTokenGetDTOOutputToken
				if err := res.ScanNamed(
					named.Required("id", &outToken.Id),
					named.Required("created_at", &outToken.CreatedAt),
					named.Required("expires_at", &outToken.ExpiresAt),
				); err != nil {
					return err
				}
				outTokens = append(outTokens, outToken)
			}
		}

		return nil
	}); err != nil {
		return domain.RefreshTokenGetDTOOutput{}, fmt.Errorf("failed to execute query transaction replace refresh token: %w", err)
	}

	return domain.RefreshTokenGetDTOOutput{
		Tokens: outTokens,
	}, nil
}

var queryAddRefreshToken = template.ReplaceAllPairs(`
DECLARE $account_id AS Int64;
DECLARE $created_at AS Datetime;
DECLARE $expires_at AS Datetime;

$to_delete = (
    SELECT
        id
    FROM 
        {{table.refresh_tokens}}
    VIEW
        {{index.account_id}}
    WHERE
        account_id = $account_id
    ORDER BY created_at DESC
    LIMIT 10000
    OFFSET {{remaining_tokens_count}}
)

DELETE FROM
    {{table.refresh_tokens}}
ON SELECT * FROM $to_delete;

INSERT INTO {{table.refresh_tokens}} (
    account_id,
    created_at,
    expires_at
)
VALUES 
(
    $account_id,
    $created_at,
    $expires_at
)
RETURNING
    id,
    created_at,
    expires_at
;
`,
	"{{table.refresh_tokens}}", tableRefreshTokens,
	"{{index.account_id}}", tableRefreshTokensIndexAccountId,
	"{{remaining_tokens_count}}", strconv.Itoa(RefreshTokensIssuedLimitation-1),
)

// TODO: describe the limitation on the amount of issued/stored refresh tokens in the API.
func (p *Token) Add(ctx context.Context, in domain.RefreshTokenAddDTOInput) (domain.RefreshTokenAddDTOOutput, error) {
	var out domain.RefreshTokenAddDTOOutput

	if err := p.db.Table().DoTx(ctx, func(ctx context.Context, tx table.TransactionActor) error {
		res, err := tx.Execute(ctx, queryDeleteRefreshTokensByAccountId, table.NewQueryParameters(
			table.ValueParam("$id", types.UTF8Value(in.AccountId)),
			table.ValueParam("$created_at", types.DatetimeValueFromTime(in.CreatedAt)),
			table.ValueParam("$expiress_at", types.DatetimeValueFromTime(in.ExpiresAt)),
		))
		if err != nil {
			return err
		}
		if err := res.Err(); err != nil {
			return err
		}
		defer func() {
			if err := res.Close(); err != nil {
				p.l.Error("failed to close ydb result", zap.Error(err))
			}
		}()

		for res.NextResultSet(ctx) {
			for res.NextRow() {
				if err := res.ScanNamed(
					named.Required("id", &out.Id),
					named.Required("created_at", &out.CreatedAt),
					named.Required("expires_at", &out.ExpiresAt),
				); err != nil {
					return err
				}
			}
		}

		return nil
	}); err != nil {
		return domain.RefreshTokenAddDTOOutput{}, fmt.Errorf("failed to execute query transaction add refresh token: %w", err)
	}

	return out, nil
}

var queryReplaceRefreshToken = template.ReplaceAllPairs(`
DECLARE $id AS Int64;
DECLARE $created_at AS Datetime;
DECLARE $expires_at AS Datetime;

$to_delete = (
    SELECT
        id,
        account_id
    FROM
        {{table.refresh_tokens}}
    WHERE id = $id
);

INSERT INTO {{table.refresh_tokens}} (
    account_id,
    created_at,
    expires_at
)
SELECT
    account_id,
    $created_at AS created_at,
    $expires_at AS expires_at
FROM $to_delete
RETURNING
    id,
    created_at,
    expires_at;

DELETE FROM {{table.refresh_tokens}}
ON SELECT id FROM $to_delete;
`,
	"{{table.refresh_tokens}}", tableRefreshTokens,
)

func (p *Token) Replace(ctx context.Context, in domain.RefreshTokenReplaceDTOInput) (domain.RefreshTokenReplaceDTOOutput, error) {
	intId, err := p.idHasher.DecodeInt64(in.Id)
	if err != nil {
		return domain.RefreshTokenReplaceDTOOutput{}, err
	}

	var out domain.RefreshTokenReplaceDTOOutput

	if err := p.db.Table().DoTx(ctx, func(ctx context.Context, tx table.TransactionActor) error {
		res, err := tx.Execute(ctx, queryDeleteRefreshTokensByAccountId, table.NewQueryParameters(
			table.ValueParam("$id", types.Int64Value(intId)),
			table.ValueParam("$created_at", types.DatetimeValueFromTime(in.CreatedAt)),
			table.ValueParam("$expiress_at", types.DatetimeValueFromTime(in.ExpiresAt)),
		))
		if err != nil {
			return err
		}
		if err := res.Err(); err != nil {
			return err
		}
		defer func() {
			if err := res.Close(); err != nil {
				p.l.Error("failed to close ydb result", zap.Error(err))
			}
		}()

		for res.NextResultSet(ctx) {
			for res.NextRow() {
				if err := res.ScanNamed(
					named.Required("id", &out.Id),
					named.Required("created_at", &out.CreatedAt),
					named.Required("expires_at", &out.ExpiresAt),
				); err != nil {
					return err
				}
			}
		}

		return nil
	}); err != nil {
		return domain.RefreshTokenReplaceDTOOutput{}, fmt.Errorf("failed to execute query transaction replace refresh token: %w", err)
	}

	return out, nil
}

var queryDeleteRefreshToken = strings.ReplaceAll(`
DECLARE $id AS Int64;

DELETE FROM
    {{table.refresh_tokens}}
WHERE
    id = $id
RETURNING id;
`,
	"{{table.refresh_tokens}}", tableRefreshTokens,
)

func (p *Token) Delete(ctx context.Context, in domain.RefreshTokenDeleteDTOInput) (domain.RefreshTokenDeleteDTOOutput, error) {
	intId, err := p.idHasher.DecodeInt64(in.Id)
	if err != nil {
		return domain.RefreshTokenDeleteDTOOutput{}, err
	}

	var out domain.RefreshTokenDeleteDTOOutput

	if err := p.db.Table().DoTx(ctx, func(ctx context.Context, tx table.TransactionActor) error {
		res, err := tx.Execute(ctx, queryDeleteRefreshTokensByAccountId, table.NewQueryParameters(
			table.ValueParam("$id", types.Int64Value(intId)),
		))
		if err != nil {
			return err
		}
		if err := res.Err(); err != nil {
			return err
		}
		defer func() {
			if err := res.Close(); err != nil {
				p.l.Error("failed to close ydb result", zap.Error(err))
			}
		}()

		for res.NextResultSet(ctx) {
			for res.NextRow() {
				if err := res.ScanNamed(
					named.Required("id", &out.Id),
				); err != nil {
					return err
				}
			}
		}

		return nil
	}); err != nil {
		return domain.RefreshTokenDeleteDTOOutput{}, fmt.Errorf("failed to execute query transaction delete refresh token: %w", err)
	}

	return out, nil
}

var queryDeleteRefreshTokensByAccountId = template.ReplaceAllPairs(`
DECLARE $account_id AS Int64;

$to_delete = (
    SELECT
        id
    FROM
       {{table.refresh_tokens}} 
    VIEW
        {{index.account_id}}
    WHERE
        account_id = $account_id
);

DELETE FROM
   {{table.refresh_tokens}} 
ON SELECT * FROM
    $to_delete
RETURNING id;
`,
	"{{table.refresh_tokens}}", tableRefreshTokens,
	"{{index.account_id}}", tableRefreshTokensIndexAccountId,
)

func (p *Token) DeleteByAccountId(ctx context.Context, in domain.RefreshTokenDeleteByAccountIdDTOInput) (domain.RefreshTokenDeleteByAccountIdDTOOutput, error) {
	outIds := make([]string, 0)

	if err := p.db.Table().DoTx(ctx, func(ctx context.Context, tx table.TransactionActor) error {
		res, err := tx.Execute(ctx, queryDeleteRefreshTokensByAccountId, table.NewQueryParameters(
			table.ValueParam("$account_id", types.UTF8Value(in.Id)),
		))
		if err != nil {
			return err
		}
		if err := res.Err(); err != nil {
			return err
		}
		defer func() {
			if err := res.Close(); err != nil {
				p.l.Error("failed to close ydb result", zap.Error(err))
			}
		}()

		for res.NextResultSet(ctx) {
			for res.NextRow() {
				var id int64
				if err := res.ScanNamed(
					named.Required("id", &id),
				); err != nil {
					return err
				}

				idStr, err := p.idHasher.EncodeInt64(id)
				if err != nil {
					return fmt.Errorf("failed to hash encode int64 id %d: %v", id, err)
				}

				outIds = append(outIds, idStr)
			}
		}

		return nil
	}); err != nil {
		return domain.RefreshTokenDeleteByAccountIdDTOOutput{}, fmt.Errorf("failed to execute query transaction delete refresh tokens by account id: %w", err)
	}

	return domain.RefreshTokenDeleteByAccountIdDTOOutput{
		Ids: outIds,
	}, nil
}
