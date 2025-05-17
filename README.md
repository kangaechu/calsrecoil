# calsrecoil

Googleカレンダーの予定を自動で処理し、記録タグを付与する自動化ツール

---

## 概要

**calsrecoil**は、Googleカレンダー上の特定条件のイベントを自動で検出し、  
指定したシェルスクリプトを並行実行、その結果をイベントのdescriptionに  
`[recorded]`または`[failed]`タグとして自動追記するツールです。

- サービスアカウント認証方式対応
- 終了時間を過ぎた予定で、descriptionに`[recorded]`や`[failed]`が含まれていないイベントが対象
- イベントごとにシェルスクリプトを自動実行（並行実行対応）
- 正常終了した場合は`[recorded]`、失敗した場合は`[failed]`を自動付与
- Googleカレンダーの予定編集にも自動対応

---

## 必要条件

- Go 1.20 以降
- Google Cloud Consoleで「サービスアカウント」作成
- サービスアカウント用JSONキー（例：`service-account.json`）
- サービスアカウントを対象カレンダーに「編集者」として共有済み
- シェルスクリプトのパス指定
- カレンダーID

---

## サービスアカウントの設定手順

1. **Google Cloud Consoleでプロジェクト作成**
2. **サービスアカウント作成**
  - [IAMと管理] → [サービスアカウント] → 新規作成
  - 「キー」タブからJSONキーを生成・ダウンロード（例：`service-account.json`）
3. **対象のGoogleカレンダーを開き、「設定と共有」→「特定のユーザーと共有」で**
  - サービスアカウントのメールアドレスを「編集者」として追加

---

## セットアップ

1. **このリポジトリをクローン**

    ```sh
    git clone https://github.com/your-account/calsrecoil.git
    cd calsrecoil
    ```

2. **依存パッケージのインストール**

    ```sh
    go mod tidy
    ```

3. **`service-account.json` をプロジェクトディレクトリに配置**

4. **必要な値を`main.go`などで設定**
  - カレンダーID
  - サービスアカウントのJSONファイルパス
  - シェルスクリプトのパス
  - タイムゾーン

---

## 使い方

```sh
go run main.go
