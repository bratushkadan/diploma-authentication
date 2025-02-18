resource "yandex_ydb_database_serverless" "this" {
  name        = "${local.common_name}-db"
  description = "auth service serverless ydb"
}

resource "yandex_ydb_topic" "test_topic" {
  database_endpoint = yandex_ydb_database_serverless.this.ydb_full_endpoint
  name              = "test-topic"

  supported_codecs       = ["raw", "gzip"]
  partitions_count       = 1
  retention_period_hours = 1

  consumer {
    name             = "test-topic-consumer-1"
    supported_codecs = ["raw", "gzip"]
  }
}

resource "yandex_iam_service_account" "app" {
  name        = "${local.common_name}-app"
  description = "application sa"
}

resource "yandex_iam_service_account" "auth_caller" {
  name        = "${local.common_name}-auth-caller"
  description = "auth service caller sa"
}
resource "yandex_resourcemanager_folder_iam_member" "auth_caller_container_invoker" {
  folder_id = local.folder_id

  role   = "serverless-containers.containerInvoker"
  member = "serviceAccount:${yandex_iam_service_account.auth_caller.id}"
}

resource "yandex_resourcemanager_folder_iam_member" "app_lockbox_payload_viewer" {
  folder_id = local.folder_id

  role   = "lockbox.payloadViewer"
  member = "serviceAccount:${yandex_iam_service_account.app.id}"
}
resource "yandex_resourcemanager_folder_iam_member" "app_ydb_writer" {
  folder_id = local.folder_id

  role   = "ydb.editor"
  member = "serviceAccount:${yandex_iam_service_account.app.id}"
}
resource "yandex_resourcemanager_folder_iam_member" "app_ymq_reader" {
  folder_id = local.folder_id

  role   = "ymq.reader"
  member = "serviceAccount:${yandex_iam_service_account.app.id}"
}
resource "yandex_resourcemanager_folder_iam_member" "app_ymq_writer" {
  folder_id = local.folder_id

  role   = "ymq.writer"
  member = "serviceAccount:${yandex_iam_service_account.app.id}"
}
resource "yandex_resourcemanager_folder_iam_member" "app_serverless_mdb_user" {
  folder_id = local.folder_id

  role   = "serverless.mdbProxies.user"
  member = "serviceAccount:${yandex_iam_service_account.app.id}"
}
resource "yandex_resourcemanager_folder_iam_member" "app_kafka_api_client" {
  folder_id = local.folder_id

  role   = "ydb.kafkaApi.client"
  member = "serviceAccount:${yandex_iam_service_account.app.id}"
}
resource "yandex_resourcemanager_folder_iam_member" "app_yds_viewer" {
  folder_id = local.folder_id

  role   = "yds.viewer"
  member = "serviceAccount:${yandex_iam_service_account.app.id}"
}
resource "yandex_resourcemanager_folder_iam_member" "app_yds_writer" {
  folder_id = local.folder_id

  role   = "yds.writer"
  member = "serviceAccount:${yandex_iam_service_account.app.id}"
}
// For Serverless Containers
resource "yandex_resourcemanager_folder_iam_member" "app_images_puller" {
  folder_id = local.folder_id

  role   = "container-registry.images.puller"
  member = "serviceAccount:${yandex_iam_service_account.app.id}"
}


resource "yandex_lockbox_secret" "app_sa_static_key" {
  name        = "${local.common_name}-app-sa-static-key-secret"
  description = "static key secret for application sa for serverless ymq type standard practicum course lab"
}

resource "yandex_iam_service_account_key" "app_sa" {
  service_account_id = yandex_iam_service_account.app.id
  description        = "auth key for app sa ${yandex_iam_service_account.app.name}"

  output_to_lockbox {
    secret_id             = yandex_lockbox_secret.app_sa_static_key.id
    entry_for_private_key = "auth_key"
  }
}
resource "yandex_iam_service_account_static_access_key" "app_sa" {
  service_account_id = yandex_iam_service_account.app.id
  description        = "static access key for app sa"

  output_to_lockbox {
    secret_id            = yandex_lockbox_secret.app_sa_static_key.id
    entry_for_access_key = "access_key_id"
    entry_for_secret_key = "secret_access_key"
  }
}

resource "yandex_iam_service_account" "ydb_ymq_manager" {
  name        = "${local.common_name}-ydb-ymq-manager"
  description = "ymq and ydb manager for serverless ymq type standard practicum course tests"
}
resource "yandex_resourcemanager_folder_iam_member" "manager_ydb_admin" {
  folder_id = local.folder_id

  role   = "ydb.admin"
  member = "serviceAccount:${yandex_iam_service_account.ydb_ymq_manager.id}"
}
resource "yandex_resourcemanager_folder_iam_member" "manager_ymq_admin" {
  folder_id = local.folder_id

  role   = "ymq.admin"
  member = "serviceAccount:${yandex_iam_service_account.ydb_ymq_manager.id}"
}

resource "yandex_lockbox_secret" "ydb_ymq_manager_sa_static_key" {
  name        = "${local.common_name}-ydb-ymq-manager-sa-static-key"
  description = "static key secret for ydb/ymq manager sa for serverless ymq type standard practicum course lab"
}

resource "yandex_iam_service_account_static_access_key" "ydb_ymq_manager_sa" {
  service_account_id = yandex_iam_service_account.ydb_ymq_manager.id
  description        = "static access key for ydb/ymq management"

  output_to_lockbox {
    secret_id            = yandex_lockbox_secret.ydb_ymq_manager_sa_static_key.id
    entry_for_access_key = "access_key_id"
    entry_for_secret_key = "secret_access_key"
  }
}

resource "yandex_container_registry" "default" {
  name      = "e-com-platform"
  folder_id = local.folder_id
}

resource "yandex_container_repository" "auth_account_repository" {
  name = "${yandex_container_registry.default.id}/auth/account"
}
resource "yandex_container_repository" "auth_email_confirmation_repository" {
  name = "${yandex_container_registry.default.id}/auth/email-confirmation"
}
