package variant

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
	"unicode"
)

// NameGenerator generates unique, pronounceable names for GOAD entities.
type NameGenerator struct {
	usedNames map[string]bool

	domainPrefixes     []string
	domainSuffixes     []string
	firstNames         []string
	lastNames          []string
	hostnamePrefixes   []string
	hostnameSuffixes   []string
	groupThemes        []string
	groupSuffixes      []string
	ouRegions          []string
	ouDivisions        []string
	animals            []string
	subdomainWords     []string
	cityNames          []string
	crackablePasswords []string // rockyou-style passwords for Kerberoastable/AS-REP accounts
}

// NewNameGenerator creates a new NameGenerator with default word lists.
func NewNameGenerator() *NameGenerator {
	return &NameGenerator{
		usedNames: make(map[string]bool),
		domainPrefixes: []string{
			"zenith", "apex", "nexus", "vertex", "prism", "quantum",
			"stellar", "fusion", "titan", "phoenix", "omega", "delta",
			"sigma", "vector", "matrix", "vortex", "cipher", "atlas",
		},
		domainSuffixes: []string{
			"corp", "tech", "systems", "solutions", "global", "industries",
			"ventures", "enterprises", "group", "labs", "dynamics", "works",
		},
		firstNames: []string{
			"James", "Michael", "Robert", "John", "David", "William",
			"Richard", "Joseph", "Thomas", "Charles", "Christopher", "Daniel",
			"Matthew", "Anthony", "Mark", "Donald", "Steven", "Paul",
			"Andrew", "Joshua", "Kenneth", "Kevin", "Brian", "George",
			"Timothy", "Ronald", "Edward", "Jason", "Jeffrey", "Ryan",
			"Jacob", "Gary", "Nicholas", "Eric", "Jonathan", "Stephen",
			"Larry", "Justin", "Scott", "Brandon", "Benjamin", "Samuel",
			"Raymond", "Gregory", "Alexander", "Patrick", "Frank", "Dennis",
			"Mary", "Patricia", "Jennifer", "Linda", "Barbara", "Elizabeth",
			"Susan", "Jessica", "Sarah", "Karen", "Nancy", "Lisa",
			"Betty", "Margaret", "Sandra", "Ashley", "Kimberly", "Emily",
			"Donna", "Michelle", "Dorothy", "Carol", "Amanda", "Melissa",
			"Deborah", "Stephanie", "Rebecca", "Sharon", "Laura", "Cynthia",
			"Kathleen", "Amy", "Angela", "Shirley", "Anna", "Brenda",
			"Pamela", "Emma", "Nicole", "Helen", "Samantha", "Katherine",
			"Christine", "Debra", "Rachel", "Carolyn", "Janet", "Catherine",
		},
		lastNames: []string{
			"Smith", "Johnson", "Williams", "Brown", "Jones", "Garcia",
			"Miller", "Davis", "Rodriguez", "Martinez", "Hernandez", "Lopez",
			"Gonzalez", "Wilson", "Anderson", "Thomas", "Taylor", "Moore",
			"Jackson", "Martin", "Lee", "Perez", "Thompson", "White",
			"Harris", "Sanchez", "Clark", "Ramirez", "Lewis", "Robinson",
			"Walker", "Young", "Allen", "King", "Wright", "Scott",
			"Torres", "Nguyen", "Hill", "Flores", "Green", "Adams",
			"Nelson", "Baker", "Hall", "Rivera", "Campbell", "Mitchell",
			"Carter", "Roberts", "Gomez", "Phillips", "Evans", "Turner",
			"Diaz", "Parker", "Cruz", "Edwards", "Collins", "Reyes",
			"Stewart", "Morris", "Morales", "Murphy", "Cook", "Rogers",
			"Gutierrez", "Ortiz", "Morgan", "Cooper", "Peterson", "Bailey",
			"Reed", "Kelly", "Howard", "Ramos", "Kim", "Cox",
			"Ward", "Richardson", "Watson", "Brooks", "Chavez", "Wood",
			"James", "Bennett", "Gray", "Mendoza", "Ruiz", "Hughes",
			"Price", "Alvarez", "Castillo", "Sanders", "Patel", "Myers",
		},
		hostnamePrefixes: []string{
			"aurora", "phoenix", "summit", "cascade", "horizon", "alpine",
			"delta", "echo", "nova", "terra", "luna", "solar",
			"atlas", "titan", "nexus", "zenith", "vertex", "apex",
			"quantum", "cipher", "vector", "matrix", "prism", "vortex",
			"beacon", "sentinel", "guardian", "fortress", "citadel", "bastion",
		},
		hostnameSuffixes: []string{
			"srv", "node", "host", "sys", "hub", "core",
			"prod", "dev", "test", "app", "db", "web",
		},
		groupThemes: []string{
			"Operations", "Engineering", "Security", "Analytics", "Development",
			"Infrastructure", "Platform", "Services", "Systems", "Management",
			"Administration", "Executive", "Leadership", "Research", "Support",
		},
		groupSuffixes: []string{
			"Team", "Group", "Unit", "Squad", "Staff",
		},
		ouRegions: []string{
			"Americas", "EMEA", "APAC", "Europe", "Pacific", "Atlantic",
			"Northern", "Southern", "Eastern", "Western", "Central",
		},
		ouDivisions: []string{
			"Operations", "Engineering", "Sales", "Marketing", "Finance",
			"HR", "IT", "Legal", "Corporate", "Research",
		},
		animals: []string{
			"Phoenix", "Griffin", "Falcon", "Eagle", "Hawk", "Raven",
			"Wolf", "Bear", "Lion", "Tiger", "Panther", "Leopard",
			"Cobra", "Viper", "Python", "Raptor", "Condor", "Vulture",
		},
		subdomainWords: []string{
			"ops", "dev", "prod", "test", "stage", "corp", "hq",
			"services", "apps", "data", "cloud", "platform",
		},
		cityNames: []string{
			"Boston", "Chicago", "Dallas", "Denver", "Houston",
			"Phoenix", "Seattle", "Portland", "Austin", "Atlanta",
			"Miami", "Philadelphia", "San Diego", "San Francisco", "New York",
		},
		// Passwords from rockyou top entries — all appear verbatim in the
		// wordlist so they crack with a straight dictionary attack (no rules).
		// Used for Kerberoastable and AS-REP roastable accounts.
		crackablePasswords: []string{
			"sunshine", "trustno1", "iloveyou", "baseball", "football",
			"princess", "starwars", "whatever", "corvette", "midnight",
			"computer", "mustang", "shadow", "master", "welcome",
			"letmein", "monkey", "blaster", "yankees", "lakers",
			"password1", "superman", "qwerty", "tigger", "batman",
			"arsenal", "access14", "buster", "soccer", "pepper",
			"ginger", "thunder", "summer", "butterfly", "chelsea",
			"chocolate", "pumpkin", "sparky", "hammer", "broncos",
			"rangers", "fishing", "marlin", "bigdog", "cowboy",
			"steelers", "dolphins", "redsox", "camaro", "creative",
			"platinum", "passw0rd", "trustme", "zeppelin", "warrior",
			"phoenix",
		},
	}
}

const maxNetBIOSLength = 15

// ensureUnique adds a counter suffix if name is already used.
func (ng *NameGenerator) ensureUnique(name string) string {
	original := name
	counter := 2
	for ng.usedNames[strings.ToLower(name)] {
		name = fmt.Sprintf("%s%d", original, counter)
		counter++
	}
	ng.usedNames[strings.ToLower(name)] = true
	return name
}

// secureChoice returns a cryptographically random element from a slice.
func secureChoice(items []string) string {
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(items))))
	return items[n.Int64()]
}

// secureBool returns true with the given probability (0.0-1.0).
func secureBool(probability float64) bool {
	n, _ := rand.Int(rand.Reader, big.NewInt(1000))
	return float64(n.Int64()) < probability*1000
}

// GenerateDomainName generates a corporate-style domain name fitting NetBIOS limits.
func (ng *NameGenerator) GenerateDomainName() string {
	for range 1000 {
		prefix := secureChoice(ng.domainPrefixes)
		suffix := secureChoice(ng.domainSuffixes)
		domain := prefix + suffix
		if len(domain) <= maxNetBIOSLength {
			return ng.ensureUnique(domain)
		}
	}
	return ng.ensureUnique(secureChoice(ng.domainPrefixes))
}

// GenerateSubdomainName generates a subdomain name for child domains.
func (ng *NameGenerator) GenerateSubdomainName() string {
	return ng.ensureUnique(secureChoice(ng.subdomainWords))
}

// GenerateUsername generates a username in firstname.lastname format.
func (ng *NameGenerator) GenerateUsername() string {
	for range 1000 {
		first := secureChoice(ng.firstNames)
		last := secureChoice(ng.lastNames)
		username := strings.ToLower(first) + "." + strings.ToLower(last)
		if !ng.usedNames[username] {
			ng.usedNames[username] = true
			return username
		}
	}
	// Fallback with counter
	first := secureChoice(ng.firstNames)
	last := secureChoice(ng.lastNames)
	username := strings.ToLower(first) + "." + strings.ToLower(last)
	return ng.ensureUnique(username)
}

// GenerateGroupName generates a group name with thematic words.
func (ng *NameGenerator) GenerateGroupName() string {
	var name string
	if secureBool(0.5) {
		name = secureChoice(ng.groupThemes) + secureChoice(ng.groupSuffixes)
	} else {
		name = secureChoice(ng.groupThemes)
	}
	return ng.ensureUnique(name)
}

// GenerateOUName generates an OU name in region/division style.
func (ng *NameGenerator) GenerateOUName() string {
	var name string
	if secureBool(0.5) {
		name = secureChoice(ng.ouRegions)
	} else {
		name = secureChoice(ng.ouDivisions)
	}
	return ng.ensureUnique(name)
}

// GenerateHostname generates a realistic hostname.
func (ng *NameGenerator) GenerateHostname() string {
	var name string
	if secureBool(0.33) {
		name = secureChoice(ng.hostnamePrefixes) + "-" + secureChoice(ng.hostnameSuffixes)
	} else {
		name = secureChoice(ng.hostnamePrefixes)
	}
	return ng.ensureUnique(strings.ToLower(name))
}

// GenerateGMSAName generates a gMSA account name like "gmsaPhoenix".
func (ng *NameGenerator) GenerateGMSAName() string {
	return ng.ensureUnique("gmsa" + secureChoice(ng.animals))
}

const (
	lowerChars   = "abcdefghijklmnopqrstuvwxyz"
	upperChars   = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	digitChars   = "0123456789"
	specialChars = "!@#$%^&*()-_=+[]{}|;:,.<>?"
)

type charClasses struct {
	upper, lower, digit, special bool
}

func analyzeCharClasses(s string) charClasses {
	var cc charClasses
	for _, c := range s {
		switch {
		case unicode.IsUpper(c):
			cc.upper = true
		case unicode.IsLower(c):
			cc.lower = true
		case unicode.IsDigit(c):
			cc.digit = true
		case !unicode.IsLetter(c) && !unicode.IsDigit(c):
			cc.special = true
		}
	}
	return cc
}

func (cc charClasses) charPool() string {
	var chars string
	if cc.lower {
		chars += lowerChars
	}
	if cc.upper {
		chars += upperChars
	}
	if cc.digit {
		chars += digitChars
	}
	if cc.special {
		chars += specialChars
	}
	if chars == "" {
		chars = lowerChars
	}
	return chars
}

func (cc charClasses) seedRequired() []byte {
	var seed []byte
	if cc.upper {
		seed = append(seed, secureChoiceByte(upperChars))
	}
	if cc.lower {
		seed = append(seed, secureChoiceByte(lowerChars))
	}
	if cc.digit {
		seed = append(seed, secureChoiceByte(digitChars))
	}
	if cc.special {
		seed = append(seed, secureChoiceByte("!@#$%^&*()-_=+"))
	}
	return seed
}

// GeneratePassword generates a password matching the complexity of the original.
func (ng *NameGenerator) GeneratePassword(original string) string {
	length := len(original)
	if length == 0 {
		length = 16
	}

	cc := analyzeCharClasses(original)
	chars := cc.charPool()
	password := cc.seedRequired()

	for len(password) < length {
		password = append(password, secureChoiceByte(chars))
	}

	secureShuffle(password)
	return string(password)
}

// GenerateCrackablePassword returns a dictionary password that can be cracked
// via hashcat/john with standard wordlists (rockyou, etc.).
func (ng *NameGenerator) GenerateCrackablePassword() string {
	return secureChoice(ng.crackablePasswords)
}

// GenerateCityName returns a unique city name.
func (ng *NameGenerator) GenerateCityName() string {
	return ng.ensureUnique(secureChoice(ng.cityNames))
}

func secureChoiceByte(s string) byte {
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(s))))
	return s[n.Int64()]
}

func secureShuffle(b []byte) {
	for i := len(b) - 1; i > 0; i-- {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		j := n.Int64()
		b[i], b[j] = b[j], b[i]
	}
}
