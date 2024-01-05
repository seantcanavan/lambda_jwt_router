package lcom

type LambdaParams struct {
	IDPath        string `lambda:"path.id"`
	IDQuery       string `lambda:"query.id"`
	UserIDBody    string `json:"userId"`
	UserIDPath    string `lambda:"path.userId"`
	UserIDQuery   string `lambda:"query.userId"`
	UserTypeBody  string `json:"userType"`
	UserTypePath  string `lambda:"path.userType"`
	UserTypeQuery string `lambda:"query.userType"`
}

func (lp *LambdaParams) GetID() string {
	return chooseLongest(lp.IDPath, lp.IDQuery)
}

func (lp *LambdaParams) GetUserID() string {
	return chooseLongest(lp.UserIDPath, lp.UserIDQuery, lp.UserIDBody)
}

func (lp *LambdaParams) GetOwnerID() string {
	return chooseLongest(lp.GetID(), lp.GetUserID())
}

func (lp *LambdaParams) GetUserType() string {
	return chooseLongest(lp.UserTypePath, lp.UserTypeQuery, lp.UserTypeBody)
}

func chooseLongest(s1 ...string) string {
	if len(s1) < 1 {
		return ""
	}

	longestIndex := -1
	longestLength := -1
	for currentIndex, currentString := range s1 {
		currentLength := len(currentString)
		if currentLength > longestLength {
			longestIndex = currentIndex
			longestLength = currentLength
		}
	}

	return s1[longestIndex]
}
