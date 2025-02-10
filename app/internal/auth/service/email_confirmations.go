package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/bratushkadan/floral/internal/auth/core/domain"
	"github.com/bratushkadan/floral/internal/auth/infrastructure/email/confirmer"
	"github.com/bratushkadan/floral/pkg/conf"
	"github.com/bratushkadan/floral/pkg/entity"

	"go.uber.org/zap"
)

var (
	ErrInvalidConfirmationToken = errors.New("invalid confirmation token")
	ErrConfirmationTokenExpired = errors.New("confirmation token expired")
)

type Conf struct {
	YdbDocApiEndpoint        string
	SqsEndpoint              string
	withSqs                  bool
	DocYdbAwsAccessKeyId     string
	DocYdbAwsSecretAccessKey string
	SqsAwsAccessKeyId        string
	SqsAwsSecretAccessKey    string

	SenderEmail                          string
	SenderPassword                       string
	EmailConfirmationApiEndpointResolver confirmer.ConfirmationUrlResolver

	emailConfirmationSendTimeout time.Duration
}

func NewConf() *Conf {
	c := &Conf{}
	c.emailConfirmationSendTimeout = 5 * time.Second

	return c
}

func (c *Conf) WithDocYdb() *Conf {
	c.DocYdbAwsAccessKeyId = conf.MustEnv("AWS_ACCESS_KEY_ID")
	c.DocYdbAwsSecretAccessKey = conf.MustEnv("AWS_SECRET_ACCESS_KEY")
	c.YdbDocApiEndpoint = conf.MustEnv("YDB_DOC_API_ENDPOINT")
	return c
}

func (c *Conf) WithSqs() *Conf {
	c.SqsAwsAccessKeyId = conf.MustEnv("AWS_ACCESS_KEY_ID")
	c.SqsAwsSecretAccessKey = conf.MustEnv("AWS_SECRET_ACCESS_KEY")
	c.SqsEndpoint = conf.MustEnv("SQS_ENDPOINT")
	return c
}

func (c *Conf) WithEmail() *Conf {
	c.SenderEmail = conf.MustEnv("SENDER_EMAIL")
	c.SenderPassword = conf.MustEnv("SENDER_PASSWORD")
	endpoint := conf.MustEnv("EMAIL_CONFIRMATION_API_ENDPOINT")
	c.EmailConfirmationApiEndpointResolver = func(ctx context.Context) (*url.URL, error) {
		host, ok := EmailConfirmationHostFromContext(ctx)
		if !ok {
			return nil, errors.New("email confirmation api endpoint resolver context must have host value in order to set confirmation email")
		}

		u, err := url.Parse((&url.URL{Scheme: "https", Host: host, Path: endpoint}).String())
		if err != nil {
			return nil, fmt.Errorf("failed to parse url for email confirmation api endpoint resolver: %v", err)
		}

		return u, nil
	}
	return c
}

type keyEmailConfirmationHost int

var keyHost keyEmailConfirmationHost

func ContextWithEmailConfirmationHost(ctx context.Context, host string) context.Context {
	return context.WithValue(ctx, keyHost, host)
}

func EmailConfirmationHostFromContext(ctx context.Context) (string, bool) {
	host, ok := ctx.Value(keyHost).(string)
	return host, ok
}

func (c *Conf) Build() *Conf {
	return c
}

type EmailConfirmation struct {
	conf                      *Conf
	l                         *zap.Logger
	repo                      domain.EmailConfirmatorTokenRepo
	emailConfNotificationProv domain.EmailConfirmationsNotificationProvider
	emailConfirmer            *confirmer.Email
}

var _ domain.EmailConfirmer = (*EmailConfirmation)(nil)

type EmailConfirmationOption func(context.Context, *EmailConfirmation) error

func WithLogger(logger *zap.Logger) EmailConfirmationOption {
	return func(_ context.Context, c *EmailConfirmation) error {
		c.l = logger
		return nil
	}
}
func WithEmailer() EmailConfirmationOption {
	return func(_ context.Context, c *EmailConfirmation) error {
		c.emailConfirmer = confirmer.New(
			c.conf.SenderEmail,
			c.conf.SenderPassword,
			c.conf.EmailConfirmationApiEndpointResolver,
		)
		return nil
	}
}
func WithRepo(repo domain.EmailConfirmatorTokenRepo) EmailConfirmationOption {
	return func(ctx context.Context, c *EmailConfirmation) error {
		c.repo = repo
		return nil
	}
}

func WithNotificationProvider(prov domain.EmailConfirmationsNotificationProvider) EmailConfirmationOption {
	return func(ctx context.Context, c *EmailConfirmation) error {
		c.emailConfNotificationProv = prov
		return nil
	}
}

func New(ctx context.Context, conf *Conf, opts ...EmailConfirmationOption) (*EmailConfirmation, error) {
	emailConfirmation := &EmailConfirmation{conf: conf}

	for _, applyOpt := range opts {
		if err := applyOpt(ctx, emailConfirmation); err != nil {
			return nil, fmt.Errorf("failed to apply option to email confirmation service: %v", err)
		}
	}

	return emailConfirmation, nil
}

func (c *EmailConfirmation) Confirm(ctx context.Context, token string) error {
	c.l.Info("confirm email")
	c.l.Info("retrieve confirmation token records")
	record, err := c.repo.FindTokenRecord(ctx, token)
	if err != nil {
		return fmt.Errorf("failed to retrieve tokens: %v", err)
	}
	if record == nil {
		c.l.Info("invalid email confirmation token record")
		return ErrInvalidConfirmationToken
	}
	c.l.Info("retrieved email confirmation token record", zap.String("email", record.Email))
	if time.Now().After(record.ExpiresAt) {
		return ErrConfirmationTokenExpired
	}
	c.l.Info("validated email confirmation token record", zap.String("email", record.Email))

	if _, err := c.emailConfNotificationProv.Send(ctx, domain.SendEmailConfirmationNotificationsDTOInput{Email: record.Email}); err != nil {
		return fmt.Errorf("failed to produce email confirmation message: %v", err)
	}
	c.l.Info("produced email confirmation message", zap.String("email", record.Email))

	c.l.Info("confirmed email", zap.String("email", record.Email))
	return nil
}

func (c *EmailConfirmation) Send(ctx context.Context, email string) error {
	c.l.Info("create confirmation token and send email", zap.String("email", email))
	tokenString := entity.Id(64)
	err := c.repo.InsertToken(ctx, email, tokenString)
	if err != nil {
		return fmt.Errorf("failed to insert confirmation token: %v", err)
	}
	c.l.Info("inserted confirmation token", zap.String("email", email))

	ctx, cancel := context.WithTimeout(ctx, c.conf.emailConfirmationSendTimeout)
	defer cancel()
	if err := c.emailConfirmer.Send(ctx, email, tokenString); err != nil {
		return fmt.Errorf("failed to send confirmation email: %v", err)
	}
	c.l.Info("sent confirmation email")

	return nil
}
