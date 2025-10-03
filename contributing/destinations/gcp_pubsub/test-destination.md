```sh
# 0. (optional) Create a GCP project (or use existing one)
gcloud projects create outpost-pubsub-test --name="Outpost PubSub Test"
gcloud config set project outpost-pubsub-test
gcloud services enable pubsub.googleapis.com

# 1. Create a topic
gcloud pubsub topics create outpost-destination-test

# 2. Create a subscription (to verify messages are received)
gcloud pubsub subscriptions create outpost-destination-test-sub --topic=outpost-destination-test

# 3. Create a service account for Outpost
gcloud iam service-accounts create outpost-pubsub-destination-sa \
  --display-name="Outpost PubSub Service Account"

# 4. Grant publisher role to service account
gcloud projects add-iam-policy-binding outpost-pubsub-test \
  --member="serviceAccount:outpost-pubsub-destination-sa@outpost-pubsub-test.iam.gserviceaccount.com" \
  --role="roles/pubsub.publisher"

# 5. Create and download service account key
gcloud iam service-accounts keys create ./gcppubsub-credentials.json \
  --iam-account=outpost-pubsub-destination-sa@outpost-pubsub-test.iam.gserviceaccount.com

# 6. Get the formatted service account JSON content
cat ./gcppubsub-credentials.json | jq -c '.'

For the Outpost destination config:
- project_id: "outpost-pubsub-test"
- topic: "outpost-test-topic"
- service_account_json: (paste the JSON content from step 6)

To verify messages:
gcloud pubsub subscriptions pull outpost-destination-test-sub --auto-ack
```
