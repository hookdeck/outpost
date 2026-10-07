# GCP Pub/Sub Configuration Instructions

[Google Cloud Pub/Sub](https://cloud.google.com/pubsub) is a fully managed messaging service for sending events between independent applications.

## How to configure GCP Pub/Sub as an event destination using the gcloud CLI

To follow these steps you will need a Google Cloud project and the [gcloud CLI](https://cloud.google.com/sdk/docs/install) installed and authenticated.

1. Enable the Pub/Sub API

    ```sh
    gcloud services enable pubsub.googleapis.com --project=PROJECT_ID
    ```

2. Create a topic if one doesn't exist (optional)

    ```sh
    gcloud pubsub topics create TOPIC --project=PROJECT_ID
    ```

3. Give Outpost access to the topic as shown below for your authentication method, then configure your GCP Pub/Sub event destination with the Project ID and Topic.

### Service account key

1. Create a service account

    ```sh
    gcloud iam service-accounts create SERVICE_ACCOUNT --project=PROJECT_ID
    ```

2. Allow it to publish to the topic

    ```sh
    gcloud pubsub topics add-iam-policy-binding TOPIC --project=PROJECT_ID \
      --member="serviceAccount:SERVICE_ACCOUNT@PROJECT_ID.iam.gserviceaccount.com" \
      --role="roles/pubsub.publisher"
    ```

3. Create a key

    ```sh
    gcloud iam service-accounts keys create key.json \
      --iam-account="SERVICE_ACCOUNT@PROJECT_ID.iam.gserviceaccount.com"
    ```

4. Paste the contents of `key.json` into Service Account JSON.

<!-- visible_when auth_method=workload_identity -->
### Workload Identity Federation (no keys)

Select Workload Identity Federation as the authentication method to see the Issuer and Subject to use below.

1. Create a workload identity pool

    ```sh
    gcloud iam workload-identity-pools create POOL --project=PROJECT_ID --location=global
    ```

2. Create a provider that trusts the Issuer for this Subject only

    ```sh
    gcloud iam workload-identity-pools providers create-oidc PROVIDER --project=PROJECT_ID \
      --location=global --workload-identity-pool=POOL \
      --issuer-uri="ISSUER" \
      --attribute-mapping="google.subject=assertion.sub" \
      --attribute-condition="assertion.sub == 'SUBJECT'"
    ```

3. Allow the Subject to publish to the topic

    ```sh
    PROJECT_NUMBER=$(gcloud projects describe PROJECT_ID --format="value(projectNumber)")
    gcloud pubsub topics add-iam-policy-binding TOPIC --project=PROJECT_ID \
      --member="principal://iam.googleapis.com/projects/$PROJECT_NUMBER/locations/global/workloadIdentityPools/POOL/subject/SUBJECT" \
      --role="roles/pubsub.publisher"
    ```

4. Enter `projects/PROJECT_NUMBER/locations/global/workloadIdentityPools/POOL/providers/PROVIDER` as the Workload Identity Provider.

To act as a service account instead, grant `roles/iam.workloadIdentityUser` on the service account to the `principal://…` member from step 3, grant the service account `roles/pubsub.publisher` on the topic, and enter its email as the Service Account Email.
<!-- /visible_when -->
