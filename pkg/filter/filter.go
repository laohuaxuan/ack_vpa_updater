package filter

import (
	"regexp"

	"ack_vpa_updater/pkg/config"
)

// 处理命名空间和部署的过滤逻辑
type Filter struct {
	config             *config.Filters
	nsIncludePatterns  []*regexp.Regexp
	nsExcludePatterns  []*regexp.Regexp
	depIncludePatterns []*regexp.Regexp
	depExcludePatterns []*regexp.Regexp
}

func NewFilter(filters *config.Filters) (*Filter, error) {
	filter := &Filter{
		config:             filters,
		nsIncludePatterns:  make([]*regexp.Regexp, 0),
		nsExcludePatterns:  make([]*regexp.Regexp, 0),
		depIncludePatterns: make([]*regexp.Regexp, 0),
		depExcludePatterns: make([]*regexp.Regexp, 0),
	}

	for _, pattern := range filters.Namespaces {
		//fmt.Printf("包含命名空间: %s\n", pattern)
		re, err := regexp.Compile("^" + pattern + "$")
		if err != nil {
			return nil, err
		}
		filter.nsIncludePatterns = append(filter.nsIncludePatterns, re)
	}

	for _, pattern := range filters.ExcludeNamespaces {
		//fmt.Printf("排除命名空间: %s\n", pattern)
		re, err := regexp.Compile("^" + pattern + "$")
		if err != nil {
			return nil, err
		}
		filter.nsExcludePatterns = append(filter.nsExcludePatterns, re)
	}

	for _, pattern := range filters.Deployments {
		//fmt.Printf("包含的deployment: %s\n", pattern)
		re, err := regexp.Compile("^" + pattern + "$")
		if err != nil {
			return nil, err
		}
		filter.depIncludePatterns = append(filter.depIncludePatterns, re)
	}

	for _, pattern := range filters.ExcludeDeployments {
		//fmt.Printf("排除的deployment: %s\n", pattern)
		re, err := regexp.Compile("^" + pattern + "$")
		if err != nil {
			return nil, err
		}
		filter.depExcludePatterns = append(filter.depExcludePatterns, re)
	}

	return filter, nil
}

func (f *Filter) ShouldProcessNamespace(namespace string) bool {
	// 先检查排除的命名空间，优先级高
	for _, re := range f.nsExcludePatterns {
		if re.MatchString(namespace) {
			//fmt.Printf("命名空间：%s匹配排除正则表达式: %s\n", namespace, re.String())
			return false
		}
	}

	if len(f.nsIncludePatterns) == 0 {
		return true
	}

	for _, re := range f.nsIncludePatterns {
		if re.MatchString(namespace) {
			//fmt.Printf("命名空间：%s匹配包含正则表达式: %s\n", namespace, re.String())
			return true
		}
	}

	return false
}

func (f *Filter) ShouldProcessDeployment(namespace, deployment string) bool {
	// 先检查排除的 Deployment，优先级高
	// 检查部署是否在包含列表中
	for _, re := range f.depExcludePatterns {
		if re.MatchString(namespace + "/" + deployment) {
			//fmt.Printf("deployment：%s/%s匹配排除正则表达式: %s\n", namespace, deployment, re.String())
			return false
		}
	}

	if len(f.depIncludePatterns) == 0 {
		return true
	}

	for _, re := range f.depIncludePatterns {
		if re.MatchString(namespace + "/" + deployment) {
			//fmt.Printf("deployment：%s/%s匹配包含正则表达式: %s\n", namespace, deployment, re.String())
			return true
		}
	}

	return false
}
