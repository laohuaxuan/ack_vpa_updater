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
		re, err := regexp.Compile("^" + pattern + "$")
		if err != nil {
			return nil, err
		}
		filter.nsIncludePatterns = append(filter.nsIncludePatterns, re)
	}

	for _, pattern := range filters.ExcludeNamespaces {
		re, err := regexp.Compile("^" + pattern + "$")
		if err != nil {
			return nil, err
		}
		filter.nsExcludePatterns = append(filter.nsExcludePatterns, re)
	}

	for _, pattern := range filters.Deployments {
		re, err := regexp.Compile("^" + pattern + "$")
		if err != nil {
			return nil, err
		}
		filter.depIncludePatterns = append(filter.depIncludePatterns, re)
	}

	for _, pattern := range filters.ExcludeDeployments {
		re, err := regexp.Compile("^" + pattern + "$")
		if err != nil {
			return nil, err
		}
		filter.depExcludePatterns = append(filter.depExcludePatterns, re)
	}

	return filter, nil
}

func (f *Filter) ShouldProcessNamespace(namespace string) bool {
	for _, re := range f.nsExcludePatterns {
		if re.MatchString(namespace) {
			return false
		}
	}

	if len(f.nsIncludePatterns) == 0 {
		return true
	}

	for _, re := range f.nsIncludePatterns {
		if re.MatchString(namespace) {
			return true
		}
	}

	return false
}

func (f *Filter) ShouldProcessDeployment(deployment string) bool {
	for _, re := range f.depExcludePatterns {
		if re.MatchString(deployment) {
			return false
		}
	}

	if len(f.depIncludePatterns) == 0 {
		return true
	}

	for _, re := range f.depIncludePatterns {
		if re.MatchString(deployment) {
			return true
		}
	}

	return false
}
