package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"github.com/adrg/frontmatter"
	"github.com/nats-io/nats.go"
	"log"
	"net/http"
	"strings"
	"text/template"
	"time"
)

// Event struct pour les événements modifiés
type Event struct {
	ID          string   `json:"id"`
	Summary     string   `json:"summary"`
	Description string   `json:"description"`
	Location    string   `json:"location"`
	Start       string   `json:"dtstart"`
	End         string   `json:"dtend"`
	UID         string   `json:"uid"`
	ResourceId  []string `json:"resource_id"`
}

// Alert struct pour stocker les alertes récupérées depuis l'API
type Alert struct {
	ID         string `json:"id"`
	Email      string `json:"email"`
	ResourceID string `json:"resource_id"`
	Oll        string `json:"oll"`
}

const (
	natsURL       = nats.DefaultURL
	alertsAPIURL  = "http://localhost:8080/alerts"
	mailAPIURL    = "https://mail.edu.forestier.re/api/send"
	mailAuthToken = "TgJelxpNZXJpZMCHKKyCmNRIchKSoIcunNcgvDbX"
)

func main() {
	// Connexion à NATS
	nc, err := nats.Connect(natsURL)
	if err != nil {
		log.Fatalf("Erreur connexion NATS : %v", err)
	}
	defer nc.Close()

	// Abonnement au sujet des événements modifiés
	sub, err := nc.Subscribe("USERS.*", processMessage)
	if err != nil {
		log.Fatalf("Erreur abonnement NATS : %v", err)
	}
	defer sub.Unsubscribe()

	// Garder le programme en exécution
	select {}
}

// Embed les templates dans le binaire (nécessaire au fonctionnement)
//
//go:embed internal/templates
var embeddedTemplates embed.FS

// Fonction pour générer le contenu de l'email à partir du template
func generateEmailContent(event Event) (string, string, error) {
	templatePath := "internal/templates/event_modified.html"

	// Charger le template depuis le FS embed
	tmpl, err := template.ParseFS(embeddedTemplates, templatePath)
	if err != nil {
		return "", "", fmt.Errorf("Erreur chargement template : %v", err)
	}

	// Appliquer les données de l'événement au template
	var tpl bytes.Buffer
	err = tmpl.Execute(&tpl, event)
	if err != nil {
		return "", "", fmt.Errorf("Erreur exécution template : %v", err)
	}

	// Extraire l'objet du mail avec frontmatter
	var matter struct {
		Subject string `yaml:"subject"`
	}
	content, err := frontmatter.Parse(strings.NewReader(tpl.String()), &matter)
	if err != nil {
		return "", "", fmt.Errorf("Erreur parsing frontmatter : %v", err)
	}

	return matter.Subject, string(content), nil
}
func processMessage(msg *nats.Msg) {
	var event Event
	if err := json.Unmarshal(msg.Data, &event); err != nil {
		log.Printf("Erreur parsing JSON : %v", err)
		return
	}

	log.Printf("Événement reçu : %s", event.Summary)

	// Récupérer les alertes associées
	alerts := getAlertsForResources(event.ResourceId)

	// Envoyer des emails aux utilisateurs concernés
	for _, alert := range alerts {
		sendEmail(alert.Email, event)
	}

	msg.Ack() // Accuser réception du message
}
func getAlertsForResources(resourceIDs []string) []Alert {
	var alerts []Alert

	// Construire la requête avec les resourceIDs
	resourceIDsStr := strings.Join(resourceIDs, ",")
	url := fmt.Sprintf("%s?resource_ids=%s", alertsAPIURL, resourceIDsStr)

	resp, err := http.Get(url)
	if err != nil {
		log.Printf("Erreur requête API Config : %v", err)
		return alerts
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Réponse API Config non OK : %d", resp.StatusCode)
		return alerts
	}

	// Décoder la réponse JSON
	err = json.NewDecoder(resp.Body).Decode(&alerts)
	if err != nil {
		log.Printf(" Erreur parsing JSON alertes : %v", err)
	}

	return alerts
}
func sendEmail(to string, event Event) {
	subject, htmlContent, err := generateEmailContent(event)
	if err != nil {
		log.Printf("Erreur génération email : %v", err)
		return
	}

	emailData := map[string]string{
		"to":      to,
		"subject": subject,
		"body":    htmlContent,
	}

	jsonData, _ := json.Marshal(emailData)

	req, err := http.NewRequest("POST", mailAPIURL, strings.NewReader(string(jsonData)))
	if err != nil {
		log.Printf("Erreur création requête mail : %v", err)
		return
	}

	req.Header.Set("Authorization", "Bearer "+mailAuthToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Erreur envoi mail : %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		log.Printf("Email envoyé à %s", to)
	} else {
		log.Printf("⚠Échec envoi email (%d)", resp.StatusCode)
	}
}
